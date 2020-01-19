package main

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/javgh/sia-nbdserver/config"
	"github.com/javgh/sia-nbdserver/nbd"
	"github.com/javgh/sia-nbdserver/sia"
)

const (
	defaultSize                  = 1099511627776
	defaultHardMaxCached         = 128
	defaultSoftMaxCached         = 96
	defaultIdleIntervalSeconds   = 120
	defaultSiaDaemonAddress      = "localhost:9980"
	defaultSiaPasswordFileSuffix = ".sia/apipassword"
)

func installSignalHandlers(siaBackend *sia.Backend) {
	c := make(chan os.Signal, 3)
	signal.Notify(c, syscall.SIGINT, syscall.SIGTERM, syscall.SIGUSR1)

	for {
		sig := <-c
		switch sig {
		case syscall.SIGINT, syscall.SIGTERM:
			log.Printf("Performing fast shutdown\n")
			err := siaBackend.Shutdown(false)
			if err != nil {
				log.Fatal(err)
			}
		case syscall.SIGUSR1:
			log.Printf("Performing thorough shutdown\n")
			err := siaBackend.Shutdown(true)
			if err != nil {
				log.Fatal(err)
			}
		default:
			panic("unexpected signal")
		}
	}
}

func serve(socketPath string, exportSize uint64, backendSettings sia.BackendSettings) {
	siaBackend, err := sia.NewBackend(backendSettings)
	if err != nil {
		log.Fatal(err)
	}

	go installSignalHandlers(siaBackend)

	err = nbd.Serve(socketPath, exportSize, siaBackend)
	if err != nil {
		log.Fatal(err)
	}

	siaBackend.Wait()
}

func main() {
	socketPath, _ := config.GetSocketPath()
	size := uint64(defaultSize)
	hardMaxCached := defaultHardMaxCached
	softMaxCached := defaultSoftMaxCached
	idleIntervalSeconds := defaultIdleIntervalSeconds
	siaDaemonAddress := defaultSiaDaemonAddress
	siaPasswordFile := config.PrependHomeDirectory(defaultSiaPasswordFileSuffix)

	rootDesc := "NBD server backed by Sia storage + local cache"
	rootCmd := &cobra.Command{
		Use:   "sia-nbdserver",
		Short: rootDesc,
		Long:  fmt.Sprintf("%s.", rootDesc),
		Run: func(cmd *cobra.Command, args []string) {
			if socketPath == "" {
				fmt.Println("Default socket path is $XDG_RUNTIME_DIR/sia-nbdserver," +
					" but $XDG_RUNTIME_DIR is not set. Please specify a socket path via -u flag.")
				os.Exit(1)
			}

			backendSettings := sia.BackendSettings{
				Size:             size,
				HardMaxCached:    hardMaxCached,
				SoftMaxCached:    softMaxCached,
				IdleInterval:     time.Duration(idleIntervalSeconds * int(time.Second)),
				SiaDaemonAddress: siaDaemonAddress,
				SiaPasswordFile:  siaPasswordFile,
			}
			serve(socketPath, size, backendSettings)
		},
	}

	rootCmd.PersistentFlags().StringVarP(&socketPath, "unix", "u", socketPath,
		"unix domain socket")
	rootCmd.PersistentFlags().Uint64VarP(&size, "size", "s", size,
		"size of block device; should ideally be a multiple of 67108864 (2 ^ 26)")
	rootCmd.PersistentFlags().IntVarP(&hardMaxCached, "hard", "H", hardMaxCached,
		"hard limit for number of 64 MiB pages in the cache")
	rootCmd.PersistentFlags().IntVarP(&softMaxCached, "soft", "S", softMaxCached,
		"soft limit for number of 64 MiB pages in the cache")
	rootCmd.PersistentFlags().IntVarP(&idleIntervalSeconds, "idle", "i", idleIntervalSeconds,
		"seconds to wait before a cache page is marked idle and upload begins")
	rootCmd.PersistentFlags().StringVar(&siaPasswordFile, "sia-password-file", siaPasswordFile,
		"path to Sia API password file")
	rootCmd.PersistentFlags().StringVar(&siaDaemonAddress, "sia-daemon", siaDaemonAddress,
		"host and port of Sia daemon")

	err := rootCmd.Execute()
	if err != nil {
		log.Fatal(err)
	}
}
