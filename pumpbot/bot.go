package pumpbot

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log"
	"regexp"
	"strconv"
	"text/template"
	"time"

	bolt "go.etcd.io/bbolt"
	tele "gopkg.in/telebot.v3"

	"github.com/gotd/td/telegram"
	"github.com/gotd/td/tg"
	"github.com/prdsrm/std/messages"
	"github.com/prdsrm/std/session/postgres"
	"github.com/prdsrm/vision/dapp"
	"github.com/prdsrm/vision/internal/utils"
)

type Client struct {
	*dapp.Client
}

// Variables for database
var (
	newTokenBucket        = []byte("new-tokens-updates")
	newDexScreenerListing = []byte("new-dexscreener-listing")
	newKOTH               = []byte("new-koth")
	newRaydiumListing     = []byte("new-raydium-listing")
	// This is really useful for higher-level interface.
	buckets = [][]byte{newTokenBucket, newKOTH, newRaydiumListing, newDexScreenerListing}
)

type NewDexScreenerEventMessage struct {
	Name        string
	Ticker      string
	Link        string
	Description string
	Backtick    string
	*dapp.DSNewPair
}

type NewKOTHMessage struct {
	Ticker      string
	Description string
	Backtick    string
	*NewKOTHEvent
}

type NewRaydiumMessage struct {
	Ticker      string
	Description string
	Backtick    string
	*dapp.NewRaydiumPool
}

type NewTokenEventMessage struct {
	Ticker                   string
	Description              string
	CA                       string
	CreatorAddress           string
	CreatorAddressBalance    string
	CreatorAddressBalanceUSD string
	BoughtSOL                string
	Backtick                 string
}

type MainMenuMessage struct {
	FirstName   string
	NewTokens   string
	DexScreener string
	Raydium     string
	KOTH        string
	Backtick    string
}

var (
	mainMenuMessage = template.Must(template.New("mainmenu").Parse(`👋 __Welcome__, {{.Backtick}}{{.FirstName}}{{.Backtick}}\!

Here in *Pump Vision bot* you can set up your custom pump\.fun tokens tracking\!

{{.NewTokens}}
{{.DexScreener}}
{{.Raydium}}
{{.KOTH}}

__New Tokens & Dexscreener & Raydium Updates Trackings & King of the Hill__:
> Tracks all pump\.fun created tokens to raydium pool creation & dexscreener paid updates, as well
> as new king of the hill
`))

	newTokenEventMessage = template.Must(template.New("tokenevent").Parse(`*🔥 NEW PUMP FUN COIN CREATED ${{.Ticker}}*
├ 🗒  *Description*: {{.Backtick}}{{.Description}}{{.Backtick}}
├ ➡️ *CA*: {{.Backtick}}{{.CA}}{{.Backtick}}
├ 👤 *Dev address*: {{.Backtick}}{{.CreatorAddress}}{{.Backtick}}
└ 💸 *Dev SOL balance*: {{.Backtick}}{{.CreatorAddressBalance}} SOL = {{.CreatorAddressBalanceUSD}} USD{{.Backtick}}
└ 💸 *The dev bought*: {{.Backtick}}{{.BoughtSOL}}{{.Backtick}} SOL
`))

	newRaydiumPoolMessage = template.Must(template.New("raydiumevent").Parse(`*🔥 ${{.Ticker}} HIT RAYDIUM\!*
├ 🗒  *Description*: {{.Backtick}}{{.Description}}{{.Backtick}}
├ ➡️ *CA*: {{.Backtick}}{{.CA}}{{.Backtick}}

🔗 [Buy on Raydium]({{.RaydiumURL}})
`))

	newKOTHMessage = template.Must(template.New("kothevent").Parse(`*🔥 NEW KING OF THE HILL: ${{.Ticker}} \!*
├ 🗒  *Description*: {{.Backtick}}{{.Description}}{{.Backtick}}
├ ➡️ *CA*: {{.Backtick}}{{.CA}}{{.Backtick}}
├ 💵 *MC*: {{.Backtick}}{{.MC}}${{.Backtick}}
`))

	newDexScreenerEventMessage = template.Must(template.New("dexscreenerEvent").Parse(`*🔥 ${{.Ticker}} DEXSCREENER LISTING\!*
├ 🗒  *Description*: {{.Backtick}}{{.Description}}{{.Backtick}}
├ ➡️ *CA*: {{.Backtick}}{{.Address}}{{.Backtick}}

🔗 [DexScreener]({{.Link}})
`))

	raydiumSubscribeMessage     = `🟢Listening for Raydium updates`
	dexScreenerSubscribeMessage = `🟢Listening for DexScreener updates`
	newTokenSubscribeMessage    = `🟢Listening for new tokens updates`
	newKOTHSubscribeMessage     = `🟢Listening for new king of the hill updates`
	dexScreenerStopMessage      = `🔴Stopped listening for DexScreener updates`
	newTokenStopMessage         = `🔴Stopped listening for new tokens updates`
	raydiumStopMessage          = `🔴Stopped listening for Raydium updates`
	kothStopMessage             = `🔴Stopped listening for new king of the hill updates`
)

func NewApp(tokenAddress string, amount int, logo string) {
	app := dapp.NewApp(tokenAddress, amount, "PUMPBOT_TOKEN", "DEBUG_PUMPBOT_TOKEN", buckets, logo)
	client := Client{
		Client: app,
	}
	// Start listening for updates
	go client.Subscribe()
	client.Run(client.SendMainMenuToUser)
}

func (client *Client) SendMainMenuToUser(ctx tele.Context) error {
	reply := client.NewMarkup()

	btnPumpFun := reply.Text("🔽 Pump.fun Tokens Updates 🔽")
	pumpTextRow := reply.Row(btnPumpFun)
	btnNew := reply.Data("Pairs", "newpump", "")
	client.Handle(&btnNew, client.StartSubscribingForNewToken, client.CheckWallet)
	btnDexScreenerPumpFun := reply.Data("DexS", "dexpump", "")
	client.Handle(&btnDexScreenerPumpFun, client.StartSubscribingForDexScreener, client.CheckWallet)
	btnRaydium := reply.Data("Raydium", "raydiumpump", "")
	client.Handle(&btnRaydium, client.StartSubscribingForRaydium, client.CheckWallet)
	btnKOTH := reply.Data("KOTH", "koth", "")
	client.Handle(&btnKOTH, client.StartSubscribingForKOTH, client.CheckWallet)
	pumpRow := reply.Row(btnNew, btnDexScreenerPumpFun, btnRaydium, btnKOTH)

	btnStopUpdate := reply.Text("🔽 Stop Pump.fun Tokens Updates 🔽")
	btnStopNewTokens := reply.Data("🛑", "stopnewtokens")
	client.Handle(&btnStopNewTokens, client.StopNewTokensSignal, client.CheckWallet)
	stopRowText := reply.Row(btnStopUpdate)
	btnStopDexscreener := reply.Data("🛑", "stopdexscreener", "")
	client.Handle(&btnStopDexscreener, client.StopDexScreenerSignal, client.CheckWallet)
	btnStopRaydium := reply.Data("🛑", "stopraydium")
	client.Handle(&btnStopRaydium, client.StopRaydiumSignal, client.CheckWallet)
	btnStopKOTH := reply.Data("🛑", "stopkoth")
	client.Handle(&btnStopKOTH, client.StopKOTHSignal, client.CheckWallet)
	stopRow := reply.Row(btnStopNewTokens, btnStopDexscreener, btnStopRaydium, btnStopKOTH)

	btnSocialMediasText := reply.Text("🔽 Socials 🔽")
	socialMediasTextRow := reply.Row(btnSocialMediasText)
	btnX := reply.URL("⌨️ X", "https://x.com//PumpVisionX")
	btnTele := reply.URL("💬 Telegram", "https://t.me/pumpvisionTG")
	btnWeb := reply.URL("🕸  Web", "https://pumpvision.tech")
	socialsRow := reply.Row(btnX, btnTele, btnWeb)

	btnProfile := reply.Data("🆔 Profile", "profile", "")
	client.Handle(&btnProfile, client.ShowProfile)
	profileRow := reply.Row(btnProfile)
	reply.Inline(pumpTextRow, pumpRow, stopRowText, stopRow, socialMediasTextRow, socialsRow, profileRow)

	newMessages := newTokenStopMessage
	raydiumMessage := raydiumStopMessage
	dexScreenerMessage := dexScreenerStopMessage
	kothMessage := kothStopMessage
	err := client.Database.Update(func(tx *bolt.Tx) error {
		for _, key := range [][]byte{newTokenBucket, newRaydiumListing, newDexScreenerListing, newKOTH} {
			b := tx.Bucket(key)
			if b != nil {
				data := b.Get([]byte(strconv.FormatInt(ctx.Chat().ID, 10)))
				if data != nil {
					switch string(key) {
					case string(newTokenBucket):
						newMessages = newTokenSubscribeMessage
					case string(newRaydiumListing):
						raydiumMessage = raydiumSubscribeMessage
					case string(newDexScreenerListing):
						dexScreenerMessage = dexScreenerSubscribeMessage
					case string(newKOTH):
						kothMessage = newKOTHSubscribeMessage
					}
				}
			}
		}
		return nil
	})
	if err != nil {
		return err
	}

	data := MainMenuMessage{
		FirstName:   ctx.Sender().FirstName,
		NewTokens:   newMessages,
		Raydium:     raydiumMessage,
		DexScreener: dexScreenerMessage,
		KOTH:        kothMessage,
		Backtick:    "`",
	}
	var buf bytes.Buffer
	err = mainMenuMessage.Execute(&buf, data)
	if err != nil {
		return err
	}
	caption := buf.String()
	picture := &tele.Photo{File: tele.FromURL(client.Logo), Caption: caption}

	user := ctx.Recipient()
	_, err = client.Send(user, picture, reply, tele.ModeMarkdownV2)
	if err != nil {
		return err
	}
	return nil
}

func (client *Client) StartSubscribingForRaydium(ctx tele.Context) error {
	return client.StartSubscribingForBucket(newRaydiumListing, ctx, raydiumSubscribeMessage)
}

func (client *Client) StartSubscribingForDexScreener(ctx tele.Context) error {
	return client.StartSubscribingForBucket(newDexScreenerListing, ctx, dexScreenerSubscribeMessage)
}

func (client *Client) StartSubscribingForNewToken(ctx tele.Context) error {
	return client.StartSubscribingForBucket(newTokenBucket, ctx, newTokenSubscribeMessage)
}

func (client *Client) StartSubscribingForKOTH(ctx tele.Context) error {
	return client.StartSubscribingForBucket(newKOTH, ctx, newKOTHSubscribeMessage)
}

func (client *Client) StopDexScreenerSignal(ctx tele.Context) error {
	return client.StopSubscribingToBucket(ctx, newDexScreenerListing, dexScreenerStopMessage)
}

func (client *Client) StopRaydiumSignal(ctx tele.Context) error {
	return client.StopSubscribingToBucket(ctx, newRaydiumListing, raydiumStopMessage)
}

func (client *Client) StopNewTokensSignal(ctx tele.Context) error {
	return client.StopSubscribingToBucket(ctx, newTokenBucket, newTokenStopMessage)
}

func (client *Client) StopKOTHSignal(ctx tele.Context) error {
	return client.StopSubscribingToBucket(ctx, newKOTH, kothStopMessage)
}

func (client *Client) HandleSubscribeEvent(subscribeEvent *dapp.NewTokenEvent, errChan chan error) {
	creatorBalance, err := client.GetSOLBalance(subscribeEvent.CreatorAddress)
	if err != nil {
		errChan <- fmt.Errorf("can't get sol balance of user: %w", err)
		return
	}
	meta, err := client.GetTokenMetadata(subscribeEvent.Address)
	if err != nil {
		errChan <- err
		return
	}
	reply := client.GetReplyWithSocialMediasAndTradingBots(subscribeEvent.Address, meta)
	creatorBalanceUSD := float64(creatorBalance) * 135
	data := NewTokenEventMessage{
		Ticker:                   meta.Symbol,
		Description:              meta.Description,
		CA:                       fmt.Sprint(subscribeEvent.Address),
		CreatorAddress:           subscribeEvent.CreatorAddress,
		CreatorAddressBalance:    fmt.Sprint(creatorBalance),
		CreatorAddressBalanceUSD: fmt.Sprint(creatorBalanceUSD),
		BoughtSOL:                subscribeEvent.BoughtSOL,
		Backtick:                 "`",
	}
	var buf bytes.Buffer
	err = newTokenEventMessage.Execute(&buf, data)
	if err != nil {
		errChan <- fmt.Errorf("can't compile new token event message template: %w", err)
		return
	}

	errChan <- client.SendMessageToEachSubscribedMemberOfBucket(newTokenBucket, meta, reply, buf.String())
}

func (client *Client) Subscribe() error {
	dexscreener := make(chan *dapp.DSNewPair)
	raydium := make(chan *dapp.NewRaydiumPool)
	newToken := make(chan *dapp.NewTokenEvent)
	newKOTHEvent := make(chan *NewKOTHEvent)
	errChan := make(chan error)
	go client.ListenForTelegramUpdates(errChan, dexscreener, raydium, newToken, newKOTHEvent)
	for {
		select {
		case newTokenEvent := <-newToken:
			go client.HandleSubscribeEvent(newTokenEvent, errChan)
		case raydiumEvent := <-raydium:
			go client.HandleRaydiumEvent(raydiumEvent, errChan)
		case dexScreenerEvent := <-dexscreener:
			go client.HandleDexScreenerEvent(dexScreenerEvent, errChan)
		case koth := <-newKOTHEvent:
			go client.HandleKOTHEvent(koth, errChan)
		case err := <-errChan:
			if err != nil {
				log.Println("error detected: ", err)
				time.Sleep(100 * time.Millisecond)
			}
		}
	}
}

func (client *Client) HandleKOTHEvent(newKOTHEvent *NewKOTHEvent, errChan chan error) {
	log.Println("received signal: ", newKOTHEvent)
	meta, err := client.GetTokenMetadata(newKOTHEvent.CA)
	if err != nil {
		errChan <- err
		return
	}
	reply := client.GetReplyWithSocialMediasAndTradingBots(newKOTHEvent.CA, meta)

	data := NewKOTHMessage{
		Ticker:       utils.ParseTicker(meta.Symbol),
		Description:  meta.Description,
		Backtick:     "`",
		NewKOTHEvent: newKOTHEvent,
	}
	var buf bytes.Buffer
	err = newKOTHMessage.Execute(&buf, data)
	if err != nil {
		errChan <- err
		return
	}
	errChan <- client.SendMessageToEachSubscribedMemberOfBucket(newKOTH, meta, reply, buf.String())
}

type PayloadNoKey struct {
	Method string `json:"method"`
}

type Payload struct {
	Method string   `json:"method"`
	Keys   []string `json:"keys"`
}

func (client *Client) ListenForTelegramUpdates(errChan chan error, dexscreener chan *dapp.DSNewPair, raydium chan *dapp.NewRaydiumPool, newTokenEvent chan *dapp.NewTokenEvent, newKOTHEvent chan *NewKOTHEvent) {
	db, err := postgres.OpenDBConnection()
	if err != nil {
		log.Fatalln("failed to open database connection: ", err)
	}
	botModel, err := postgres.GetRandomBot(db)
	if err != nil {
		log.Fatalln("can't get random bot: ", err)
	}
	listen := func(ctx context.Context, client *telegram.Client, dispatcher tg.UpdateDispatcher, options telegram.Options) error {
		dapp.Join(ctx, client)
		done := make(chan bool)
		messages.MonitorMessages(ctx, client, dispatcher, func(chatID int64, m *tg.Message) error {
			if chatID == 2130372878 {
				signal, exists := dapp.ParseDSNewPair(m)
				if exists {
					dexscreener <- signal
				}
			} else if chatID == 2030160217 {
				signal, exists := ParseRaydium(m)
				if exists {
					raydium <- signal
				}
			} else if chatID == 2240136729 {
				signal, exists := ParseNewToken(m)
				if exists {
					newTokenEvent <- signal
				}
			} else if chatID == 2130573958 {
				signal, exists := ParseNewKOTH(m)
				if exists {
					newKOTHEvent <- signal
				}
			}
			return nil
		}, done)
		<-done
		return nil
	}
	err = postgres.ConnectToBotFromDatabase(db, botModel, listen)
	if err != nil {
		log.Fatalln("can't connect: ", err)
	}
}

func (client *Client) GetReplyWithSocialMediasAndTradingBots(address string, meta *dapp.TokenMetadata) *tele.ReplyMarkup {
	reply := client.NewMarkup()
	btnSocialMedia := reply.Text("🔽 Social Medias 🔽")
	socialMediaTextRow := reply.Row(btnSocialMedia)
	btnX := reply.URL("⌨️  X", meta.Twitter)
	btnTele := reply.URL("💬 Telegram", meta.Telegram)
	btnWeb := reply.URL("🕸  Web", meta.Website)
	linkRow := reply.Row(btnX, btnTele, btnWeb)
	btnTradingBots := reply.Text("🔽 Buy with trading bots 🔽")
	textTradingRow := reply.Row(btnTradingBots)
	btnTrojanBot := reply.URL("💸 Trojan", fmt.Sprintf("https://t.me/solana_trojanbot?start=r-strikism-%s", address))
	btnMaestroBot := reply.URL("💸 Maestro", fmt.Sprintf("https://t.me/maestro?start=r-strikism-%s", address))
	tradingRow := reply.Row(btnMaestroBot, btnTrojanBot)
	reply.Inline(socialMediaTextRow, linkRow, textTradingRow, tradingRow)
	return reply
}

func (client *Client) HandleRaydiumEvent(raydiumEvent *dapp.NewRaydiumPool, errChan chan error) {
	log.Println("received signal: ", raydiumEvent)
	meta, err := client.GetTokenMetadata(raydiumEvent.CA)
	if err != nil {
		errChan <- err
		return
	}
	reply := client.GetReplyWithSocialMediasAndTradingBots(raydiumEvent.CA, meta)

	data := NewRaydiumMessage{
		Ticker:         utils.ParseTicker(meta.Symbol),
		Description:    meta.Description,
		Backtick:       "`",
		NewRaydiumPool: raydiumEvent,
	}
	var buf bytes.Buffer
	err = newRaydiumPoolMessage.Execute(&buf, data)
	if err != nil {
		errChan <- err
		return
	}
	errChan <- client.SendMessageToEachSubscribedMemberOfBucket(newRaydiumListing, meta, reply, buf.String())
}

func (client *Client) HandleDexScreenerEvent(dexScreenerEvent *dapp.DSNewPair, errChan chan error) {
	if !dexScreenerEvent.PumpFun {
		errChan <- errors.New("not a pump.fun token")
		return
	}

	log.Println("received pair: ", dexScreenerEvent)
	meta, err := client.GetTokenMetadata(dexScreenerEvent.Address)
	if err != nil {
		errChan <- err
		return
	}
	reply := client.GetReplyWithSocialMediasAndTradingBots(dexScreenerEvent.Address, meta)

	link := fmt.Sprintf("https://dexscreener.com/solana/%s", dexScreenerEvent.Address)
	data := NewDexScreenerEventMessage{
		Name:        meta.Name,
		Ticker:      utils.ParseTicker(meta.Symbol),
		Description: meta.Description,
		Link:        link,
		Backtick:    "`",
		DSNewPair:   dexScreenerEvent,
	}
	var buf bytes.Buffer
	err = newDexScreenerEventMessage.Execute(&buf, data)
	if err != nil {
		errChan <- fmt.Errorf("can't compile dexScreenerMessage template: %v: %w", data, err)
		return
	}

	errChan <- client.SendMessageToEachSubscribedMemberOfBucket(newDexScreenerListing, meta, reply, buf.String())
}

type NewKOTHEvent struct {
	Name         string
	Date         string
	CA           string
	CreationDate string
	MC           string
	Buys         string
	Sells        string
}

func ParseNewKOTH(m *tg.Message) (*NewKOTHEvent, bool) {
	text := utils.RemoveSpacesAndNewlines(m.Message)
	var re = regexp.MustCompile(`(?m)(.+)becomethekingofhillthe([a-zA-Z0-9,]+2024[0-9]{1,2}:[0-9]{1,2})(AM|PM)MINTCA([1-9A-HJ-NP-Za-km-z]{32,44}).+Createdat:([a-zA-Z0-9,]+2024[0-9]{1,2}:[0-9]{1,2})(AM|PM)Marketcap:\$(\d+)Trades:(\d+)txs\|(\d+)Replies:(\d+)`)
	for _, match := range re.FindAllStringSubmatch(text, -1) {
		name := match[1]
		date := match[2]
		ca := match[4]
		creationDate := match[5]
		mc := match[7]
		buys := match[8]
		sells := match[9]
		kothEvent := NewKOTHEvent{
			Name:         name,
			Date:         date,
			CA:           ca,
			CreationDate: creationDate,
			MC:           mc,
			Buys:         buys,
			Sells:        sells,
		}
		return &kothEvent, true
	}
	return nil, false
}

func ParseNewToken(message *tg.Message) (*dapp.NewTokenEvent, bool) {
	text := utils.RemoveSpacesAndNewlines(message.Message)
	var newToken dapp.NewTokenEvent
	var re = regexp.MustCompile(`(?m)NEWPUMPFUNPOOLMINTCA([1-9A-HJ-NP-Za-km-z]{32,44})CREATORCreator:([1-9A-HJ-NP-Za-km-z]{8})Thecreatorboughtfor([0-9.]+)SOLof([a-zA-Z$]+)\|`)
	for _, match := range re.FindAllStringSubmatch(text, -1) {
		token := match[1]
		sol := match[3]
		ticker := match[4]
		newToken.Address = token
		newToken.BoughtSOL = sol
		newToken.Ticker = ticker
		for _, entity := range message.Entities {
			switch msgEntity := entity.(type) {
			case *tg.MessageEntityTextURL:
				newToken.CreatorAddress = msgEntity.URL[27:]
				return &newToken, true
			}
		}
	}
	return nil, false
}

func ParseRaydium(message *tg.Message) (*dapp.NewRaydiumPool, bool) {
	text := utils.RemoveSpacesAndNewlines(message.Message)
	var re1 = regexp.MustCompile(`(?m)RAYDIUMPOOLDEPLOYEDMINTCA([1-9A-HJ-NP-Za-km-z]{32,44})pump((\$|)[a-zA-Z,-]+)`)
	var re2 = regexp.MustCompile(`(?m)NEWCURVECOMPLETED([1-9A-HJ-NP-Za-km-z]{32,44})pump([a-zA-Z,-]+)\|`)
	for _, re := range []*regexp.Regexp{re1, re2} {
		for _, match := range re.FindAllStringSubmatch(text, -1) {
			address := match[1] + "pump"
			link := fmt.Sprintf("https://raydium.io/swap/?outputMint=%s&inputMint=sol", address)
			ticker := match[2]
			data := dapp.NewRaydiumPool{CA: address, RaydiumURL: link, Ticker: ticker}
			return &data, true
		}
	}
	return nil, false
}
