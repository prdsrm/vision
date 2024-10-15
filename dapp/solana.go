package dapp

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"text/template"
	"time"

	"github.com/go-resty/resty/v2"
	bolt "go.etcd.io/bbolt"
	tele "gopkg.in/telebot.v3"

	solana "github.com/blocto/solana-go-sdk/client"
	"github.com/blocto/solana-go-sdk/common"
	"github.com/blocto/solana-go-sdk/program/metaplex/token_metadata"
	"github.com/blocto/solana-go-sdk/rpc"
	"github.com/blocto/solana-go-sdk/types"
	"github.com/mr-tron/base58"
	"github.com/tyler-smith/go-bip39"

	"github.com/gotd/td/telegram"
	"github.com/gotd/td/tg"
	"github.com/prdsrm/std/channels"
	"github.com/prdsrm/std/dialogs"

	"github.com/prdsrm/vision/internal/telebot"
	"github.com/prdsrm/vision/internal/utils"

	"go.uber.org/zap"
)

var (
	// Universal markup builders.
	menu = &tele.ReplyMarkup{ResizeKeyboard: true}

	btnMainMenu = menu.Text("🏘 Open home(main menu)")

	noImage = "https://upload.wikimedia.org/wikipedia/commons/a/ac/No_image_available.svg"
)

type Wallet struct {
	FirstName           string
	WalletAddress       string
	PrivateKey          string
	Mnemonic            string
	ConfirmedUserWallet string
	BalanceSOL          string
	BalanceToken        string
	Backtick            string
	HoldingAmount       uint
	TokenName           string
}

var (
	startMessage           = `👋 Check out the keyboard menu, or re-run the /start command, in order to get access to this menu!`
	registeredUserMessage  = `🟢 Successfully identified your wallet!`
	walletDuplicateMessage = `🔴 Wallet detected, however, it is already registered. Please use a new wallet.`
	tokenSold              = `🔴 It seems like you sold your $%s, so, you can't use this bot anymore, you must hold them`

	holdersOnly = `⚠️ *Pump Vision bot* is for holders only

You need to hold at least %d $%s tokens to start customizing these signals as you wish\! 🔥
`
	walletMessage = template.Must(template.New("walletmessage").Parse(`🔰 **{{.FirstName}}** __profile__:

> If your wallet wasn't detected, please send a 0$ amount of SOL or any other tokens to:
💰 *Wallet Address*: {{.Backtick}}{{.WalletAddress}}{{.Backtick}}
⏺ *Mnemonic, if needed*: {{.Backtick}}{{.Mnemonic}}{{.Backtick}}

> You need to hold {{.HoldingAmount}} ${{.TokenName}} in order to access the bot:
⏺ *Detected wallet address*: {{.Backtick}}{{.ConfirmedUserWallet}}{{.Backtick}}
⏺ *SOL balance*: {{.Backtick}}{{.BalanceSOL}}{{.Backtick}}
⏺ ${{.TokenName}} balance: {{.Backtick}}{{.BalanceToken}}{{.Backtick}}
`))
)

func GetUserKey(ctx tele.Context) []byte {
	return []byte(strconv.FormatInt(ctx.Chat().ID, 10))
}

func NewApp(tokenAddress string, amount int, env string, debug string, buckets [][]byte, logo string) *Client {
	// Setting up database
	db, err := bolt.Open("data/my.db", 0600, nil)
	if err != nil {
		log.Fatal("Couldn't initialize database.", err)
	}

	// Setting up Solana RPC client
	url := os.Getenv("HELIUS_RPC_URL")
	if url == "" {
		url = rpc.MainnetRPCEndpoint
		log.Fatal("no helius = slow")
	}
	solana := solana.NewClient(url)

	httpClient := resty.New()

	name := env
	if os.Getenv("DEBUG") == "1" {
		name = debug
	}
	token := os.Getenv(name)
	bot := telebot.GetBot(token)

	rpc := make(chan func() error)
	client := Client{
		Bot:      bot,
		Http:     httpClient,
		Database: db,
		Solana:   solana,
		RPC:      rpc,

		TokenAddress: tokenAddress,
		Logo:         logo,
	}

	log.Println("Creating buckets if they don't exists")
	bucketsToCheck := append(buckets, metadataCacheBucket)
	for _, bucket := range bucketsToCheck {
		err := CheckIfBucketExistsOrCreateIt(client.Database, bucket)
		if err != nil {
			log.Fatal(err)
		}
	}

	log.Println("Starting the Solana RPC rate limiter.")
	go client.SolanaRPCRateLimiter()
	client.TokenSymbol = "VISION"
	client.HoldingAmount = uint(amount)

	log.Println("Starting to routinely check for our users addresses.")
	go client.CheckUsersWallets(buckets)

	return &client
}

var metadataCacheBucket = []byte("metadata-cache")

type Client struct {
	*tele.Bot
	Database *bolt.DB
	Solana   *solana.Client
	Http     *resty.Client
	RPC      chan func() error

	MainMenu      tele.HandlerFunc
	TokenAddress  string
	TokenSymbol   string
	HoldingAmount uint
	Logo          string
}

func (client *Client) GetSOLBalance(publicKey string) (uint64, error) {
	var balance uint64
	updated := false
	err := client.Database.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(metadataCacheBucket)
		if b != nil {
			updated = true
			data := b.Get([]byte(publicKey))
			if data != nil {
				balance = binary.LittleEndian.Uint64(data)
			}
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	if balance != 0 || updated {
		return balance, nil
	}
	client.RPC <- func() error {
		log.Println("Getting sol balance for address", zap.String("address", publicKey))
		// Get amount of Solana found in wallet
		var accountBalance uint64
		balance, err := client.Solana.GetBalance(
			context.TODO(),
			publicKey,
		)
		if err != nil {
			return err
		}
		// Get the amount of token found in wallet.
		accountBalance = balance / 1e9

		err = client.Database.Update(func(tx *bolt.Tx) error {
			b := tx.Bucket(metadataCacheBucket)
			balance := make([]byte, 8)
			binary.LittleEndian.PutUint64(balance, accountBalance)
			return b.Put([]byte(publicKey), balance)
		})
		if err != nil {
			return err
		}

		return nil
	}
	for i := 0; i < 20; i++ {
		var balance uint64
		updated := false
		err := client.Database.View(func(tx *bolt.Tx) error {
			b := tx.Bucket(metadataCacheBucket)
			if b != nil {
				updated = true
				data := b.Get([]byte(publicKey))
				if data != nil {
					balance = binary.LittleEndian.Uint64(data)
				}
			}
			return nil
		})
		if err != nil {
			return 0, err
		}
		if balance != 0 || updated {
			log.Println("SOL balance found", zap.String("address", publicKey), zap.Uint64("balance", balance))
			return balance, nil
		}

		time.Sleep(500 * time.Millisecond)
	}
	return 0, errors.New("not found, within 10 seconds")
}

type Entity struct {
	ID string
}

func NewEntity(id string) *Entity {
	return &Entity{ID: id}
}

func (entity *Entity) Recipient() string {
	return entity.ID
}

func (client *Client) CheckUsersWallets(buckets [][]byte) {
	min := 10
	max := 20
	randomNumber := rand.Intn(max-min+1) + min
	limiter := time.Tick(time.Duration(randomNumber) * time.Minute)
	for {
		<-limiter
		for _, bucket := range buckets {
			log.Println("Checking for users in bucket", zap.String("bucket", string(bucket)))
			err := CheckIfBucketExistsOrCreateIt(client.Database, bucket)
			if err != nil {
				log.Println("can't create bucket", zap.Error(err))
				continue
			}
			err = client.Database.Update(func(tx *bolt.Tx) error {
				updateBucket := tx.Bucket(bucket)
				return updateBucket.ForEach(func(key, _ []byte) error {
					log.Println("Checking for bucket with key", zap.String("user_id", string(key)))
					userBucket := tx.Bucket(key)
					if userBucket != nil {
						publicKey := string(userBucket.Get([]byte("confirmedPublicKey")))
						log.Println("Checking public key", zap.String("publicKey", publicKey))
						if publicKey == "" {
							log.Println("Client sold his tokens", zap.String("userID", string(key)))
							entity := NewEntity(string(key))
							_, err := client.Send(entity, fmt.Sprintf(tokenSold, client.TokenSymbol))
							if err != nil {
								return err
							}
							log.Println("Deleting update for user.")
							err = updateBucket.Delete(key)
							if err != nil {
								return err
							}
							log.Println("Setting the user as non-confirmed.")
							err = userBucket.Delete([]byte("confirmedPublicKey"))
							if err != nil {
								return err
							}
							return nil
						}
						// NOTE: sleeping 100 millisecond for each check in case I have too much users filling the queue.
						time.Sleep(100 * time.Millisecond)
						_, tokenBalance, err := client.GetSolanaAndTokenBalance(publicKey, client.TokenAddress)
						if err != nil {
							return err
						}
						if tokenBalance < uint64(client.HoldingAmount) {
							log.Println("Client sold his tokens", zap.String("userID", string(key)))
							entity := NewEntity(string(key))
							_, err := client.Send(entity, fmt.Sprintf(tokenSold, client.TokenSymbol))
							if err != nil {
								return err
							}
							log.Println("Setting the user as non-confirmed.")
							err = userBucket.Delete([]byte("confirmedPublicKey"))
							if err != nil {
								return err
							}
							log.Println("Deleting update for user.")
							err = updateBucket.Delete(key)
							if err != nil {
								return err
							}
						}
					}
					return nil
				})
			})
			if err != nil {
				log.Println("can't loop over users", zap.Error(err))
			}
		}
	}
}

func (client *Client) SolanaRPCRateLimiter() {
	// 50 requests per seconds with Helius. 1000 / 50 = 20ms
	limiter := time.Tick(20 * time.Millisecond)
	for {
		<-limiter
		select {
		case rpc := <-client.RPC:
			go func() {
				err := rpc()
				if err != nil {
					log.Println("error while running rpc request", zap.Error(err))
				}
			}()
		}
	}
}

func (client *Client) getTokenMetadataRPCRequest(tokenAddress string) func() error {
	return func() error {
		log.Println("Getting token metadata for account.", zap.String("address", tokenAddress))
		mint := common.PublicKeyFromString(tokenAddress)
		metadataAccount, err := token_metadata.GetTokenMetaPubkey(mint)
		if err != nil {
			return err
		}

		// get data which stored in metadataAccount
		accountInfo, err := client.Solana.GetAccountInfo(context.Background(), metadataAccount.ToBase58())
		if err != nil {
			return fmt.Errorf("failed to get accountInfo, err: %v", err)
		}
		log.Println("Found account info.", zap.Any("info", accountInfo))

		metadata, err := token_metadata.MetadataDeserialize(accountInfo.Data)
		if err != nil {
			err = client.Database.Update(func(tx *bolt.Tx) error {
				b := tx.Bucket(metadataCacheBucket)
				return b.Put([]byte(tokenAddress), []byte("err"))
			})
			if err != nil {
				return fmt.Errorf("can't update cache: %w", err)
			}
			return nil
		}
		log.Println("Found token metadata", zap.Any("meta", metadata))

		err = CheckIfBucketExistsOrCreateIt(client.Database, metadataCacheBucket)
		if err != nil {
			return fmt.Errorf("can't check if bucket exists: %w", err)
		}
		err = client.Database.Update(func(tx *bolt.Tx) error {
			b := tx.Bucket(metadataCacheBucket)
			log.Println("Setting address to uri", zap.String("tokenAddress", metadata.Data.Uri))
			return b.Put([]byte(tokenAddress), []byte(metadata.Data.Uri))
		})
		if err != nil {
			return fmt.Errorf("can't update cache: %w", err)
		}
		return nil
	}
}

func (client *Client) GetTokenMetadata(tokenAddress string) (*TokenMetadata, error) {
	uri, err := client.GetURIFromCache(tokenAddress)
	if err != nil {
		return nil, err
	}
	var tokenMetadata TokenMetadata
	resp, err := client.Http.R().Get(uri)
	if err != nil {
		return nil, fmt.Errorf("can't get uri: %s: %w", string(resp.Body()), err)
	}
	err = json.Unmarshal(resp.Body(), &tokenMetadata)
	if err != nil {
		return nil, fmt.Errorf("can't unmarshal text: %s: %w", string(resp.Body()), err)
	}
	tokenMetadata.Symbol = utils.ParseTicker(tokenMetadata.Symbol)
	return &tokenMetadata, nil
}

func (client *Client) GetURIFromCache(address string) (string, error) {
	err := CheckIfBucketExistsOrCreateIt(client.Database, metadataCacheBucket)
	if err != nil {
		return "", err
	}
	var uri string
	for i := 0; i < 10; i++ {
		err = client.Database.View(func(tx *bolt.Tx) error {
			b := tx.Bucket(metadataCacheBucket)
			if b != nil {
				data := b.Get([]byte(address))
				if data != nil {
					uri = string(data)
					return nil
				}
			}
			return nil
		})
		if err != nil {
			return "", err
		}
		if uri != "" && uri != "err" {
			return uri, nil
		}
		// We do it after checking if URI is correct, because it is possible that we already cached
		// the token metadata URI.
		if i == 0 {
			log.Println("Not cached, getting token metadata.", zap.String("address", address))
			client.RPC <- client.getTokenMetadataRPCRequest(address)
		}
		if uri == "err" {
			log.Println("Waiting 20 seconds, token metadata not present yet.", zap.String("address", address))
			time.Sleep(20 * time.Second)
			client.RPC <- client.getTokenMetadataRPCRequest(address)
		} else {
			time.Sleep(200 * time.Millisecond)
		}
	}
	return "", errors.New("not found")
}

func CheckIfBucketExistsOrCreateIt(db *bolt.DB, bucket []byte) error {
	return db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucket)
		if b == nil {
			newBucket, err := tx.CreateBucket(bucket)
			if err != nil {
				return fmt.Errorf("create bucket: %s", err)
			}
			b = newBucket
		}
		return nil
	})
}

func (client *Client) CheckInCache(publicKey string, tokenAddr string) (bool, uint64, uint64, error) {
	// First, we check in the cache if it already exists
	var balance uint64
	updated := false
	err := client.Database.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(metadataCacheBucket)
		if b != nil {
			updated = true
			data := b.Get([]byte(fmt.Sprintf("%s%s", publicKey, tokenAddr)))
			if data != nil {
				balance = binary.LittleEndian.Uint64(data)
			}
		}
		return nil
	})
	if err != nil {
		return false, 0, 0, err
	}
	if balance != 0 || updated {
		log.Println("Token balance found", zap.String("wallet", publicKey), zap.String("token", tokenAddr), zap.Uint64("tokenBalance", balance))
		solBalance, err := client.GetSOLBalance(publicKey)
		if err != nil {
			return false, 0, 0, err
		}
		return true, solBalance, balance, nil
	}
	return false, 0, 0, errors.New("unexpected error while searching for balance & token balance in cache")
}

func (client *Client) GetSolanaAndTokenBalance(publicKey string, tokenAddr string) (uint64, uint64, error) {
	// We only want to check in cache, if its not our token.
	// Because, if we check for our token, it means, we want to check a user balance.
	if tokenAddr != client.TokenAddress {
		exists, balance, solBalance, err := client.CheckInCache(publicKey, tokenAddr)
		if err != nil {
			return 0, 0, err
		}
		if exists {
			return balance, solBalance, nil
		}
	}

	client.RPC <- func() error {
		log.Println("Getting token account balance", zap.String("public", publicKey), zap.String("token", tokenAddr))
		tokenAccounts, err := client.Solana.GetTokenAccountsByOwnerByMint(context.TODO(), publicKey, tokenAddr)
		if err != nil {
			return fmt.Errorf("can't get amount of token in balance: %w", err)
		}
		if len(tokenAccounts) != 0 {
			tokenBalance := tokenAccounts[0].Amount / 1e5
			err = client.Database.Update(func(tx *bolt.Tx) error {
				b := tx.Bucket(metadataCacheBucket)
				balance := make([]byte, 8)
				binary.LittleEndian.PutUint64(balance, tokenBalance)
				return b.Put([]byte(fmt.Sprintf("%s%s", publicKey, tokenAddr)), balance)
			})
			if err != nil {
				return fmt.Errorf("can't update cache: %w", err)
			}
		}
		return nil
	}

	for i := 0; i < 20; i++ {
		exists, balance, solBalance, err := client.CheckInCache(publicKey, tokenAddr)
		if err != nil {
			return 0, 0, err
		}
		if exists {
			return balance, solBalance, nil
		}

		time.Sleep(500 * time.Millisecond)
	}
	return 0, 0, errors.New("not found, within 10 seconds")
}

type TokenMetadata struct {
	Name        string `json:"string"`
	Symbol      string `json:"symbol"`
	Description string `json:"description"`
	Image       string `json:"image"`
	ShowName    bool   `json:"showName"`
	CreatedOn   string `json:"createdOn"`
	Twitter     string `json:"twitter"`
	Telegram    string `json:"telegram"`
	Website     string `json:"website"`
}

type ApiResponse struct {
	Data []Price `json:"data"`
}

type Price struct {
	PriceUSD          string `json:"priceUsd"`
	Time              int64  `json:"time"`
	CirculatingSupply string `json:"circulatingSupply"`
	Date              string `json:"date"`
}

func GetSOLPrice() (float64, error) {
	url := "https://api.coincap.io/v2/assets/solana/history?interval=m1"
	resp, err := http.Get(url)
	if err != nil {
		return 0, fmt.Errorf("error fetching data: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, fmt.Errorf("error reading response body: %w", err)
	}
	var apiResponse ApiResponse
	err = json.Unmarshal(body, &apiResponse)
	if err != nil {
		return 0, fmt.Errorf("error unmarshalling JSON: %w", err)
	}

	if apiResponse.Data != nil {
		if len(apiResponse.Data) > 0 {
			f, err := strconv.ParseFloat(apiResponse.Data[0].PriceUSD, 64)
			if err != nil {
				return 0, err
			}
			return f, nil
		}
	}
	return 0, errors.New("not as expected")
}

type UserWallet struct {
	WalletAddr []byte
	PrivateKey []byte
	Mnemonic   []byte
}

func GetUsersAddressesInBucket(b *bolt.Bucket) (*UserWallet, error) {
	walletAddress := b.Get([]byte("publicKey"))
	privateKey := b.Get([]byte("privateKey"))
	mnemonic := b.Get([]byte("mnemonic"))
	if walletAddress == nil || privateKey == nil || mnemonic == nil {
		// Generate a mnemonic for memorization or user-friendly seeds
		entropy, _ := bip39.NewEntropy(256)
		newMnemonic, _ := bip39.NewMnemonic(entropy)
		// Saving the account to the database
		account := types.NewAccount()
		newPublicKey := account.PublicKey.ToBase58()
		newPrivateKey := base58.Encode(account.PrivateKey)
		err := b.Put([]byte("publicKey"), []byte(newPublicKey))
		if err != nil {
			return nil, err
		}
		err = b.Put([]byte("privateKey"), []byte(newPrivateKey))
		if err != nil {
			return nil, err
		}
		err = b.Put([]byte("mnemonic"), []byte(newMnemonic))
		if err != nil {
			return nil, err
		}
		walletAddress = []byte(newPublicKey)
		privateKey = []byte(newPrivateKey)
		mnemonic = []byte(newMnemonic)
	}
	return &UserWallet{
		WalletAddr: walletAddress,
		PrivateKey: privateKey,
		Mnemonic:   mnemonic,
	}, nil
}

func (client *Client) OnData(ctx tele.Context, work func(ctx tele.Context, userWallet string, walletAddress string, privateKey string, mnemonic string, balance uint64, tokenBalance uint64) error) error {
	var confirmed []byte
	var walletAddress []byte
	var privateKey []byte
	var mnemonic []byte
	err := client.Database.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(GetUserKey(ctx))
		if b == nil {
			newBucket, err := tx.CreateBucket(GetUserKey(ctx))
			if err != nil {
				return fmt.Errorf("create bucket: %s", err)
			}
			b = newBucket
		}
		confirmed = b.Get([]byte("confirmedPublicKey"))
		userWallet, err := GetUsersAddressesInBucket(b)
		if err != nil {
			return err
		}
		walletAddress = userWallet.WalletAddr
		privateKey = userWallet.PrivateKey
		mnemonic = userWallet.Mnemonic
		return nil
	})
	if confirmed != nil {
		accountBalance, tokenBalance, err := client.GetSolanaAndTokenBalance(string(confirmed), client.TokenAddress)
		if err != nil {
			return err
		}
		err = work(ctx, string(confirmed), string(walletAddress), string(privateKey), string(mnemonic), accountBalance, tokenBalance)
		if err != nil {
			return err
		}
		return nil
	}

	publicKey := string(walletAddress)
	signatures, err := client.Solana.GetSignaturesForAddress(context.TODO(), publicKey)
	if err != nil {
		return err
	}
	var balance uint64
	var tokensInBalance uint64
	if len(signatures) != 0 {
		// Taking last transaction
		signature := signatures[0]

		transaction, err := client.Solana.GetTransaction(context.TODO(), signature.Signature)
		if err != nil {
			return err
		}
		log.Println("Transaction", zap.Any("transaction", transaction))
		// Since the first key in the account keys is always the sender
		senderKey := transaction.AccountKeys[0]
		log.Println("Sender", zap.String("sender", senderKey.String()), zap.String("publicKey", publicKey))
		if senderKey.String() != publicKey {
			wallet, err := client.CheckIfWalletIsDuplicate(senderKey.String(), string(GetUserKey(ctx)))
			if err != nil {
				return err
			}
			// If the wallet has already been found in the database, while its not confirmed for the user, this is really weird.
			if wallet {
				return ctx.Send(walletDuplicateMessage)
			}
			accountBalance, tokenBalance, err := client.GetSolanaAndTokenBalance(senderKey.String(), client.TokenAddress)
			if err != nil {
				return err
			}
			balance = accountBalance
			tokensInBalance = tokenBalance

			err = client.Database.Update(func(tx *bolt.Tx) error {
				b := tx.Bucket(GetUserKey(ctx))
				if tokenBalance > uint64(client.HoldingAmount) {
					log.Println("Registering user", zap.Int64("chat_id", ctx.Chat().ID), zap.String("wallet", senderKey.String()))
					err = b.Put([]byte("confirmedPublicKey"), []byte(senderKey.String()))
					if err != nil {
						return err
					}
					err := ctx.Send(registeredUserMessage)
					if err != nil {
						return err
					}
				}
				return nil
			})
			if err != nil {
				return err
			}
		}
	}
	err = work(ctx, "", publicKey, string(privateKey), string(mnemonic), balance, tokensInBalance)
	if err != nil {
		return err
	}
	return nil
}

func (client *Client) CheckWallet(next tele.HandlerFunc) tele.HandlerFunc {
	return func(ctx tele.Context) error {
		if ctx.Chat().ID == 6718073837 || ctx.Chat().ID == 6932075241 {
			return next(ctx)
		}
		err := client.OnData(ctx, func(ctx tele.Context, userWallet string, walletAddress string, privateKey string, mnemonic string, balance uint64, tokenBalance uint64) error {
			if tokenBalance < uint64(client.HoldingAmount) {
				return nil
			}
			return nil
		})
		if err != nil {
			return err
		}
		return next(ctx)
	}
}

func (client *Client) RedirectToProfile(ctx tele.Context) error {
	reply := client.NewMarkup()
	btnProfile := reply.Data("🆔 Profile", "profile", "")
	client.Handle(&btnProfile, client.ShowProfile)
	row := reply.Row(btnProfile)
	reply.Inline(row)
	err := ctx.Send(fmt.Sprintf(holdersOnly, client.HoldingAmount, client.TokenSymbol), reply, tele.ModeMarkdownV2)
	if err != nil {
		return err
	}
	return nil
}

func (client *Client) ShowProfile(ctx tele.Context) error {
	err := client.OnData(ctx, func(ctx tele.Context, userWallet string, walletAddress string, privateKey string, mnemonic string, balance uint64, tokenBalance uint64) error {
		wallet := Wallet{
			FirstName:           ctx.Sender().FirstName,
			WalletAddress:       walletAddress,
			PrivateKey:          privateKey,
			Mnemonic:            mnemonic,
			ConfirmedUserWallet: userWallet,
			BalanceSOL:          fmt.Sprint(balance),
			BalanceToken:        fmt.Sprint(tokenBalance),
			HoldingAmount:       client.HoldingAmount, // Assuming holdingAmount is defined elsewhere
			TokenName:           client.TokenSymbol,   // Assuming tokenName is defined elsewhere
			Backtick:            "`",
		}
		return client.ShowIt(ctx, wallet)
	})
	if err != nil {
		log.Println("Can't show profile to user", zap.Int64("user", ctx.Chat().ID), zap.Error(err))
		return err
	}
	return nil
}

func (client *Client) StartSubscribingForBucket(bucket []byte, ctx tele.Context, message string) error {
	err := client.Database.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucket)
		if b == nil {
			newBucket, err := tx.CreateBucket(bucket)
			if err != nil {
				return fmt.Errorf("create bucket: %s", err)
			}
			b = newBucket
		}
		if b != nil {
			err := b.Put([]byte(strconv.FormatInt(ctx.Chat().ID, 10)), []byte("start"))
			if err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	return ctx.Send(message)
}

func (client *Client) CheckIfWalletIsDuplicate(walletAddress string, userKey string) (bool, error) {
	exists := false
	err := client.Database.View(func(tx *bolt.Tx) error {
		return tx.ForEach(func(name []byte, b *bolt.Bucket) error {
			if name != nil {
				if string(name) != userKey {
					data := tx.Bucket(name)
					publicKey := data.Get([]byte("confirmedUserPublicKey"))
					if publicKey != nil {
						if string(publicKey) == walletAddress {
							exists = true
						}
					}
				}
			}
			return nil
		})
	})
	if err != nil {
		return false, err
	}
	return exists, nil
}

func (client *Client) ShowIt(ctx tele.Context, wallet Wallet) error {
	var buf bytes.Buffer
	err := walletMessage.Execute(&buf, wallet)
	if err != nil {
		return err
	}
	picture := &tele.Photo{File: tele.FromURL(client.Logo), Caption: buf.String()}
	user := ctx.Recipient()
	_, err = client.Send(user, picture, tele.ModeMarkdownV2)
	if err != nil {
		return err
	}
	return nil
}

func (client *Client) welcome(ctx tele.Context) error {
	tags := ctx.Args()
	for _, tag := range tags {
		log.Println("Got following argument from start command:", tag)
	}
	err := CheckIfBucketExistsOrCreateIt(client.Database, GetUserKey(ctx))
	if err != nil {
		return err
	}
	err = client.MainMenu(ctx)
	if err != nil {
		return err
	}
	return ctx.Send(startMessage, menu)
}
func (client *Client) StopSubscribingToBucket(ctx tele.Context, bucket []byte, msg string) error {
	err := client.Database.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucket)
		if b != nil {
			err := b.Delete([]byte(strconv.FormatInt(ctx.Chat().ID, 10)))
			if err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	return ctx.Send(msg)
}

func (client *Client) SendMessageToEachSubscribedMemberOfBucket(bucket []byte, meta *TokenMetadata, reply *tele.ReplyMarkup, caption string) error {
	return client.Database.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucket)
		// 30 messages per second max, 1000/30=around 33ms delay.
		limiter := time.Tick(35 * time.Millisecond)
		return b.ForEach(func(key, _ []byte) error {
			<-limiter
			go func() error {
				if key != nil {
					userID := string(key)
					entity := NewEntity(userID)
					picture := &tele.Photo{File: tele.FromURL(meta.Image), Caption: caption, Width: 640, Height: 360}
					_, err := client.Send(entity, picture, reply, tele.ModeMarkdownV2)
					if err != nil {
						logo := &tele.Photo{File: tele.FromURL(noImage), Caption: caption}
						if strings.Contains(meta.Image, "cf-ipfs.com") {
							resp, err := client.Http.R().Get(meta.Image)
							if err == nil {
								image := resp.Body()
								reader := bytes.NewReader(image)
								logo = &tele.Photo{File: tele.FromReader(reader), Caption: caption, Width: 640, Height: 360}
							}
						}
						_, err := client.Send(entity, logo, reply, tele.ModeMarkdownV2)
						if err != nil {
							return fmt.Errorf("error while sending message: %w", err)
						}
					}
					log.Println("Sent message for token.", zap.String("token", meta.Symbol), zap.String("image", meta.Image), zap.String("user", userID))
				}
				return nil
			}()
			return nil
		})
	})
}

func (client Client) Run(mainMenu tele.HandlerFunc) {
	menu.Reply(
		menu.Row(btnMainMenu),
	)

	// Global commands
	client.MainMenu = mainMenu
	client.Handle("/start", client.welcome)
	client.Handle(&btnMainMenu, client.MainMenu)

	client.Start()
}

type DSNewPair struct {
	Chain        string
	Address      string
	PumpFun      bool
	PriceUSD     string
	PriceSOL     string
	FDV          string
	Liquidity    string
	Ticker       string
	PooledToken  string
	PooledSOL    string
	PooledSOLUSD string
}

func ParseDSNewPair(message *tg.Message) (*DSNewPair, bool) {
	text := utils.RemoveSpacesAndNewlines(message.Message)
	var re = regexp.MustCompile(`(?m)NewPaironSolana:.+\/(SOL).+Tokenaddress:([1-9A-HJ-NP-Za-km-z]{32,44})PriceUSD:\$([0-9.,]+)Price:([0-9.,]+)SOLFDV:\$([0-9.,]+)Totalliquidity:\$([0-9.,]+)Pooled($|)([A-Z]+):([0-9.,]+)PooledSOL:([0-9.,]+)\(\$([0-9.,]+)\)WARNING:buyingnewpairsisextremelyrisky,pleasereadpinnedmessagebeforeproceeding!`)
	for _, match := range re.FindAllStringSubmatch(text, -1) {
		chain := match[1]
		address := match[2]
		pumpfun := false
		if strings.Contains(address, "pump") {
			pumpfun = true
		}
		priceUSD := match[3]
		priceSOL := match[4]
		fdv := match[5]
		liquidity := match[6]
		ticker := match[8]
		pooledToken := match[9]
		pooledSOL := match[10]
		pooledSOLUSD := match[11]

		pair := DSNewPair{
			Chain:        chain,
			Address:      address,
			PumpFun:      pumpfun,
			PriceUSD:     priceUSD,
			PriceSOL:     priceSOL,
			FDV:          fdv,
			Liquidity:    liquidity,
			Ticker:       ticker,
			PooledToken:  pooledToken,
			PooledSOL:    pooledSOL,
			PooledSOLUSD: pooledSOLUSD,
		}
		return &pair, true
	}
	return nil, false
}

type NewRaydiumPool struct {
	Ticker     string
	CA         string
	RaydiumURL string
}

type NewTokenEvent struct {
	Address        string `json:"address"`
	CreatorAddress string `json:"creator_address"`
	BoughtSOL      string `json:"creator_bought"`
	Ticker         string `json:"ticker"`
}

type TokensResponse struct {
	SchemaVersion string `json:"schemaVersion"`
	Pairs         []Pair `json:"pairs"`
}

type Pair struct {
	ChainID       string      `json:"chainID"`
	DexID         string      `json:"dexId"`
	URL           string      `json:"url"`
	PairAddress   string      `json:"pairAddress"`
	BaseToken     Token       `json:"baseToken"`
	QuoteToken    Token       `json:"quoteToken"`
	PriceNative   string      `json:"priceNative"`
	PriceUsd      string      `json:"priceUsd"`
	Txns          Txns        `json:"txns"`
	DexVolume     Volume      `json:"volume"`
	PriceChange   PriceChange `json:"priceChange"`
	DexLiquidity  Liquidity   `json:"liquidity"`
	PairCreatedAt int         `json:"pairCreatedAt"`
	DexFDV        int64       `json:"fdv"`
}

type Token struct {
	Address string `json:"address"`
	Name    string `json:"name"`
	Symbol  string `json:"symbol"`
	Info    Info   `json:"info"`
}

type Txns struct {
	M5  Tx `json:"m5"`
	H1  Tx `json:"h1"`
	H6  Tx `json:"h6"`
	H24 Tx `json:"h24"`
}

type Tx struct {
	Buy  int `json:"buys"`
	Sell int `json:"sells"`
}

type Volume struct {
	H24 float32 `json:"h24"`
	H6  float32 `json:"h6"`
	H1  float32 `json:"h1"`
	M5  float32 `json:"m5"`
}

type Liquidity struct {
	USD   float32 `json:"usd"`
	Base  float32 `json:"base"`
	Quote float32 `json:"quote"`
}

type PriceChange struct {
	M5  float32 `json:"m5"`
	H1  float32 `json:"h1"`
	H6  float32 `json:"h6"`
	H24 float32 `json:"h24"`
}

type Info struct {
	ImageUrl string      `json:"imageUrl"`
	Websites interface{} `json:"websites"`
	Socials  []Social    `json:"socials"`
}

type Social struct {
	Type string `json:"type"`
	URL  string `json:"url"`
}

func (client *Client) GetTokenInformationFromDexScreener(token string) ([]Pair, error) {
	requestURL := fmt.Sprintf("https://api.dexscreener.com/latest/dex/tokens/%s", token)
	resp, err := http.Get(requestURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	var response TokensResponse
	err = json.Unmarshal(body, &response)
	if err != nil {
		return nil, err
	}
	return response.Pairs, nil
}

func Join(ctx context.Context, client *telegram.Client) {
	err := dialogs.JoinChatlist(ctx, client, "w6zBjBi0cAE1YTZk")
	if err != nil {
		log.Println("Cannot join chatlist: ", err)
	}
	for _, username := range []string{"DSNewPairsSolana", "DexMoonshotNewTokens", "dexmoonshotstracker"} {
		err := channels.JoinChannel(ctx, client, username)
		if err != nil {
			log.Println("Can't join channel: ", err)
		}
	}
}
