package moonbot

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

var (
	// Universal markup builders.
	menu = &tele.ReplyMarkup{ResizeKeyboard: true}

	btnMainMenu = menu.Text("🏘 Open home(main menu)")
)

// Variables for database
var (
	newToken              = []byte("new-tokens-updates")
	newDexScreenerListing = []byte("new-dexscreener-listing")
	newRaydiumListing     = []byte("new-raydium-listing")
	buckets               = [][]byte{newToken, newDexScreenerListing, newRaydiumListing}
)

type NewDexScreenerEventMessage struct {
	Name        string
	Ticker      string
	Link        string
	Description string
	Backtick    string
	*dapp.DSNewPair
}

type NewTokenEventMessage struct {
	Ticker      string
	Description string
	Backtick    string
	SolBalance  uint64
	*dapp.NewTokenEvent
}

type NewRaydiumMessage struct {
	Ticker      string
	Description string
	Backtick    string
	*dapp.NewRaydiumPool
}

type MainMenuMessage struct {
	FirstName   string
	NewTokens   string
	DexScreener string
	Raydium     string
	Backtick    string
}

var (
	mainMenuMessage = template.Must(template.New("mainmenu").Parse(`👋 __Welcome__, {{.Backtick}}{{.FirstName}}{{.Backtick}}\!

Here in *Moon Vision bot* you can set up your DexScreener MoonShot tokens tracking\!

{{.NewTokens}}
{{.DexScreener}}
{{.Raydium}}

__New Tokens & Dexscreener & Raydium Updates Trackings__:
> Tracks all DexScreener MoonShot newly created tokens, migration to raydium pool & dexscreener paid updates
`))

	newRaydiumPoolMessage = template.Must(template.New("raydium").Parse(`*🔥 ${{.Ticker}} HIT RAYDIUM\!*
├ ➡️ *CA*: {{.Backtick}}{{.CA}}{{.Backtick}}

🔗 [Buy on Raydium]({{.RaydiumURL}})
`))

	newDexScreenerEventMessage = template.Must(template.New("dexs").Parse(`*🔥 ${{.Ticker}} GOT A DEXSCREENER LISTING\!*
├ ➡️  *CA*: {{.Backtick}}{{.Address}}{{.Backtick}}

🔗 [DexScreener]({{.Link}})
`))

	newTokenMessage = template.Must(template.New("newtoken").Parse(`*🔥 NEW DEXSCREENER MOONSHOT TOKEN: ${{.Ticker}} \!*
├ ➡️  *CA*: {{.Backtick}}{{.Address}}{{.Backtick}}

👤 *Creator*:
├ ➡️ *Creator Address*: {{.Backtick}}{{.CreatorAddress}}{{.Backtick}}
├ 💸 *Creator SOL Balance*: {{.Backtick}}{{.SolBalance}}{{.Backtick}} SOL
├ 💸 *The creator bought for* {{.Backtick}}{{.BoughtSOL}}{{.Backtick}} *SOL of ${{.Ticker}}*

🔗 [DexScreener](https://dexscreener.com/solana/{{.Address}})
`))

	raydiumSubscribeMessage     = `🟢Listening for Raydium updates`
	dexScreenerSubscribeMessage = `🟢Listening for DexScreener updates`
	newTokenSubscribeMessage    = `🟢Listening for new tokens updates`
	dexScreenerStopMessage      = `🔴Stopped listening for DexScreener updates`
	newTokenStopMessage         = `🔴Stopped listening for new tokens updates`
	raydiumStopMessage          = `🔴Stopped listening for Raydium updates`
)

func NewApp(tokenAddress string, amount int, logo string) {
	app := dapp.NewApp(tokenAddress, amount, "MOON_TOKEN", "DEBUG_MOON_TOKEN", buckets, logo)

	client := Client{
		Client: app,
	}

	// Start listening for updates
	go client.Subscribe()

	client.Run(client.SendMainMenuToUser)
}

func (client *Client) SendMainMenuToUser(ctx tele.Context) error {
	reply := client.NewMarkup()

	btnPumpFun := reply.Text("🔽 MoonShot Tokens Updates 🔽")
	pumpTextRow := reply.Row(btnPumpFun)
	btnNewToken := reply.Data("New", "new")
	client.Handle(&btnNewToken, client.StartSubscribingForNewTokens, client.CheckWallet)
	btnDexScreenerPumpFun := reply.Data("DexS", "dex")
	client.Handle(&btnDexScreenerPumpFun, client.StartSubscribingForDexScreener, client.CheckWallet)
	btnRaydium := reply.Data("Raydium", "raydium")
	client.Handle(&btnRaydium, client.StartSubscribingForRaydium, client.CheckWallet)
	pumpRow := reply.Row(btnNewToken, btnDexScreenerPumpFun, btnRaydium)

	btnStopUpdate := reply.Text("🔽 Stop MoonShot Tokens Updates 🔽")
	stopRowText := reply.Row(btnStopUpdate)
	btnStopNew := reply.Data("🛑", "stopnew")
	client.Handle(&btnStopNew, client.StopNewTokenSignal, client.CheckWallet)
	btnStopDexscreener := reply.Data("🛑", "stopdexscreener")
	client.Handle(&btnStopDexscreener, client.StopDexScreenerSignal, client.CheckWallet)
	btnStopRaydium := reply.Data("🛑", "stopraydium")
	client.Handle(&btnStopRaydium, client.StopRaydiumSignal, client.CheckWallet)
	stopRow := reply.Row(btnStopNew, btnStopDexscreener, btnStopRaydium)

	btnSocialMediasText := reply.Text("🔽 Socials 🔽")
	socialMediasTextRow := reply.Row(btnSocialMediasText)
	btnX := reply.URL("⌨️ X", "https://x.com//MoonVisionBot")
	btnTele := reply.URL("💬 Telegram", "https://t.me/MoonVisionPortal")
	btnWeb := reply.URL("🕸  Web", "https://moonvision.tech")
	socialsRow := reply.Row(btnX, btnTele, btnWeb)

	btnProfile := reply.Data("🆔 Profile", "profile", "")
	client.Handle(&btnProfile, client.ShowProfile)
	profileRow := reply.Row(btnProfile)
	reply.Inline(pumpTextRow, pumpRow, stopRowText, stopRow, socialMediasTextRow, socialsRow, profileRow)

	newMessages := newTokenStopMessage
	raydiumMessage := raydiumStopMessage
	dexScreenerMessage := dexScreenerStopMessage
	err := client.Database.Update(func(tx *bolt.Tx) error {
		for _, key := range buckets {
			b := tx.Bucket(key)
			if b != nil {
				data := b.Get([]byte(strconv.FormatInt(ctx.Chat().ID, 10)))
				if data != nil {
					switch string(key) {
					case string(newToken):
						newMessages = newTokenSubscribeMessage
					case string(newRaydiumListing):
						raydiumMessage = raydiumSubscribeMessage
					case string(newDexScreenerListing):
						dexScreenerMessage = dexScreenerSubscribeMessage
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

func (client *Client) StartSubscribingForNewTokens(ctx tele.Context) error {
	return client.StartSubscribingForBucket(newToken, ctx, newTokenSubscribeMessage)
}

func (client *Client) StopDexScreenerSignal(ctx tele.Context) error {
	return client.StopSubscribingToBucket(ctx, newDexScreenerListing, dexScreenerStopMessage)
}

func (client *Client) StopNewTokenSignal(ctx tele.Context) error {
	return client.StopSubscribingToBucket(ctx, newToken, newTokenStopMessage)
}

func (client *Client) StopRaydiumSignal(ctx tele.Context) error {
	return client.StopSubscribingToBucket(ctx, newRaydiumListing, raydiumStopMessage)
}

func (client *Client) Subscribe() error {
	dexscreener := make(chan *dapp.DSNewPair)
	raydium := make(chan *dapp.NewRaydiumPool)
	newToken := make(chan *dapp.NewTokenEvent)
	errChan := make(chan error)
	go client.ListenForTelegramUpdates(errChan, dexscreener, raydium, newToken)
	for {
		select {
		case raydiumEvent := <-raydium:
			go client.HandleRaydiumEvent(raydiumEvent, errChan)
		case dexScreenerEvent := <-dexscreener:
			go client.HandleDexScreenerEvent(dexScreenerEvent, errChan)
		case token := <-newToken:
			go client.HandleNewTokenEvent(token, errChan)
		case err := <-errChan:
			if err != nil {
				log.Println("error: ", err)
				time.Sleep(100 * time.Millisecond)
			}
		}
	}
}

func (client *Client) ListenForTelegramUpdates(errChan chan error, dexscreener chan *dapp.DSNewPair, raydium chan *dapp.NewRaydiumPool, newToken chan *dapp.NewTokenEvent) {
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
			} else if chatID == 2247823917 {
				signal, exists := ParseRaydium(m)
				if exists {
					raydium <- signal
				}
			} else if chatID == 2219835123 {
				signal, exists := ParseNewToken(m)
				if exists {
					newToken <- signal
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
	btnTradingBots := reply.Text("🔽 Buy with trading bots 🔽")
	textTradingRow := reply.Row(btnTradingBots)
	btnTrojanBot := reply.URL("💸 Trojan", fmt.Sprintf("https://t.me/solana_trojanbot?start=r-strikism-%s", address))
	btnMaestroBot := reply.URL("💸 Maestro", fmt.Sprintf("https://t.me/maestro?start=r-strikism-%s", address))
	tradingRow := reply.Row(btnMaestroBot, btnTrojanBot)
	reply.Inline(textTradingRow, tradingRow)
	return reply
}

func (client *Client) HandleNewTokenEvent(newTokenEvent *dapp.NewTokenEvent, errChan chan error) {
	log.Println("received signal: ", newTokenEvent)
	meta, err := client.GetTokenMetadata(newTokenEvent.Address)
	if err != nil {
		errChan <- err
		return
	}
	reply := client.GetReplyWithSocialMediasAndTradingBots(newTokenEvent.Address, meta)

	solBalance, err := client.GetSOLBalance(newTokenEvent.CreatorAddress)
	if err != nil {
		errChan <- err
		return
	}

	data := NewTokenEventMessage{
		Ticker:        utils.ParseTicker(meta.Symbol),
		Description:   meta.Description,
		SolBalance:    solBalance,
		Backtick:      "`",
		NewTokenEvent: newTokenEvent,
	}
	var buf bytes.Buffer
	err = newTokenMessage.Execute(&buf, data)
	if err != nil {
		errChan <- err
		return
	}

	errChan <- client.SendMessageToEachSubscribedMemberOfBucket(newToken, meta, reply, buf.String())
}

func (client *Client) HandleRaydiumEvent(raydiumEvent *dapp.NewRaydiumPool, errChan chan error) {
	log.Println("received signal: ", raydiumEvent)
	meta, err := client.GetTokenMetadata(raydiumEvent.CA)
	if err != nil {
		errChan <- err
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
	log.Println("received pair: ", dexScreenerEvent)
	moonshot := false
	pairs, err := client.GetTokenInformationFromDexScreener(dexScreenerEvent.Address)
	for _, dpair := range pairs {
		if dpair.DexID == "moonshot" {
			moonshot = true
		}
	}
	if len(pairs) == 0 {
		errChan <- errors.New("no token found via dexscreener public API")
		return
	} else if !moonshot {
		errChan <- errors.New("this is not a dexscreener moonshot token")
		return
	}

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

func ParseNewToken(message *tg.Message) (*dapp.NewTokenEvent, bool) {
	text := utils.RemoveSpacesAndNewlines(message.Message)
	var newToken dapp.NewTokenEvent
	var re = regexp.MustCompile(`(?m)NEWDEXMOONSHOTTOKENMINTCA([1-9A-HJ-NP-Za-km-z]{32,44})CREATORCreator:([a-zA-Z0-9]+)Thecreatorboughtfor([0-9.,]+)SOLof($|)([a-zA-Z0-9.,$-_]+)\|`)
	for _, match := range re.FindAllStringSubmatch(text, -1) {
		newToken.Address = match[1]
		newToken.BoughtSOL = match[3]
		newToken.Ticker = match[5]
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
	var newToken dapp.NewRaydiumPool
	var re = regexp.MustCompile(`(?m).+\(([A-Za-z0-9$]+)\).+Website([1-9A-HJ-NP-Za-km-z]{32,44})`)
	for _, match := range re.FindAllStringSubmatch(text, -1) {
		newToken.Ticker = match[1]
		newToken.CA = match[2]
		newToken.RaydiumURL = fmt.Sprintf("https://raydium.io/swap/?outputMint=%s&inputMint=sol", newToken.CA)
		return &newToken, true
	}
	return nil, false
}
