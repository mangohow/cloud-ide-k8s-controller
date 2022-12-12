package signal

import (
	"context"
	"os"
	"os/signal"
	"syscall"
)

var onlyOneSignalHandler = make(chan struct{})

func SetupSignal(fns ...func()) context.Context {
	// 当函数被调用两次，就会panic
	close(onlyOneSignalHandler)
	sigCh := make(chan os.Signal, 2)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT)
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		<-sigCh
		for _, fn := range fns {
			fn()
		}
		cancel()
		<-sigCh
		os.Exit(1)
	}()

	return ctx
}
