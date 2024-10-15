package cmd

import (
	"strconv"

	"github.com/prdsrm/vision/moonbot"
	"github.com/prdsrm/vision/pumpbot"
	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "advertiser",
	Short: "",
}

func newPumpBotCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use: "pumpbot",
		Run: func(cmd *cobra.Command, args []string) {
			tokenAddress := cmd.Flag("address").Value.String()
			holdingAmount := cmd.Flag("count").Value.String()
			logo := cmd.Flag("logo").Value.String()
			amount, err := strconv.Atoi(holdingAmount)
			if err != nil {
				panic(err)
			}
			pumpbot.NewApp(tokenAddress, amount, logo)
		},
	}
	cmd.Flags().StringP("address", "a", "", "Token Address")
	cmd.Flags().IntP("count", "c", 500, "Holding amount")
	cmd.Flags().StringP("logo", "l", "https://upload.wikimedia.org/wikipedia/commons/5/5b/Insert_image_here.svg", "Logo")
	return cmd
}

func newMoonBotCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use: "moonbot",
		Run: func(cmd *cobra.Command, args []string) {
			tokenAddress := cmd.Flag("address").Value.String()
			holdingAmount := cmd.Flag("count").Value.String()
			logo := cmd.Flag("logo").Value.String()
			amount, err := strconv.Atoi(holdingAmount)
			if err != nil {
				panic(err)
			}
			moonbot.NewApp(tokenAddress, amount, logo)
		},
	}
	cmd.Flags().StringP("address", "a", "", "Token Address")
	cmd.Flags().IntP("count", "c", 500, "Holding amount")
	cmd.Flags().StringP("logo", "l", "https://upload.wikimedia.org/wikipedia/commons/5/5b/Insert_image_here.svg", "Logo")
	return cmd
}

func Execute() {
	// Pump bot command
	rootCmd.AddCommand(newPumpBotCommand())
	// Moon bot command
	rootCmd.AddCommand(newMoonBotCommand())

	if err := rootCmd.Execute(); err != nil {
		panic(err)
	}
}
