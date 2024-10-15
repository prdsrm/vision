package telebot

import (
	"log"
	"time"

	tele "gopkg.in/telebot.v3"
)

func GetBot(token string) *tele.Bot {
	pref := tele.Settings{
		Token:  token,
		Poller: &tele.LongPoller{Timeout: 10 * time.Second},
	}
	bot, err := tele.NewBot(pref)
	if err != nil {
		log.Fatal("Couldn't create new bot.", err)
	}
	log.Println("Created new bot", token)
	return bot
}
