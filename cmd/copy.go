package cmd

import (
	"fmt"
	"github.com/mediocregopher/radix/v3"
	"log"
	"sync"
	"time"

	"github.com/spf13/cobra"
)

var pattern string
var scanCount, report, limit int

type keyDump struct {
	key   string
	value string
	ttl   int
}

var copyCmd = &cobra.Command{
	Use:   "copy [sourceHost:port] [targetHost:port]",
	Short: "Copy keys from source redis instance to destination by given pattern",
	Long:  ``,
	Args:  cobra.MinimumNArgs(2),
	Run: func(cmd *cobra.Command, args []string) {

		fmt.Println("Start copying")

		clientSource, err := radix.DefaultClientFunc("tcp", args[0])
		if err != nil {
			log.Fatal(err)
		}

		clientTarget, err := radix.DefaultClientFunc("tcp", args[1])
		if err != nil {
			log.Fatal(err)
		}

		scanner := createScanner(clientSource)

		keyChannel := make(chan keyDump)

		waitingGroup := new(sync.WaitGroup)

		for i := 0; i < 10; i++ {
			waitingGroup.Add(1)
			go restorer(keyChannel, clientTarget, waitingGroup)
		}

		readKeys(clientSource, keyChannel, scanner)
		close(keyChannel)

		waitingGroup.Wait()

		if err := scanner.Close(); err != nil {
			log.Fatal(err)
		}

	},
}

func createScanner(clientSource radix.Client) radix.Scanner {
	scanOpts := radix.ScanOpts{
		Command: "SCAN",
		Count:   scanCount,
	}

	if pattern != "*" {
		scanOpts.Pattern = pattern
	}

	return radix.NewScanner(clientSource, scanOpts)
}

func restorer(keyChannel chan keyDump, clientTarget radix.Client, waitingGroup *sync.WaitGroup) {
	for {
		key, more := <-keyChannel

		if more {
			err := clientTarget.Do(radix.FlatCmd(nil, "RESTORE", key.key, key.ttl, key.value, "REPLACE"))
			if err != nil {
				log.Fatal(err)
			}
		} else {
			break
		}
	}

	waitingGroup.Done()
}

func readKeys(clientSource radix.Client, keyChannel chan keyDump, scanner radix.Scanner) {
	var key string
	counter := 0
	cycle := 0

	cycleStart := time.Now()
	start := time.Now()

	for scanner.Next(&key) {

		var value string
		var ttl int

		p := radix.Pipeline(
			radix.Cmd(&ttl, "PTTL", key),
			radix.Cmd(&value, "DUMP", key),
		)

		if err := clientSource.Do(p); err != nil {
			panic(err)
		}

		if ttl < 0 {
			ttl = 0
		}

		keyChannel <- keyDump{key: key, value: value, ttl: ttl}
		counter++
		cycle++

		if cycle == report {
			log.Printf("Copied another %d keys in: %s", report, time.Since(cycleStart))
			cycle = 0
			cycleStart = time.Now()
		}

		if limit > 0 && counter > limit {
			fmt.Printf("Early exit after %d keys copied\n", counter)
			return
		}
	}

	log.Printf("In total %d keys copied in %s", counter, time.Since(start))
}

func init() {
	rootCmd.AddCommand(copyCmd)

	copyCmd.Flags().StringVar(&pattern, "pattern", "*", "Match pattern for keys")
	copyCmd.Flags().IntVar(&scanCount, "scanCount", 100, "COUNT parameter for redis SCAN command")
	copyCmd.Flags().IntVar(&report, "report", 1000, "After what number of keys copied to report time")
	copyCmd.Flags().IntVar(&limit, "limit", 0, "After what number of keys copied to stop (0 - unlimited)")
}
