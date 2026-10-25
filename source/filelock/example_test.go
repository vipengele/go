package filelock_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"go.vipengele.org/filelock"
)

func ExampleNew() {
	dir, err := os.MkdirTemp("", "filelock-example-")
	if err != nil {
		fmt.Println(err)

		return
	}
	defer func() { _ = os.RemoveAll(dir) }()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	unlock, err := filelock.New(filepath.Join(dir, "app.lock")).Lock(ctx)
	if err != nil {
		fmt.Println(err)

		return
	}
	defer unlock()

	fmt.Println("locked")
	// Output: locked
}

func ExampleNewNoop() {
	unlock, err := filelock.NewNoop().Lock(context.Background())
	if err != nil {
		fmt.Println(err)

		return
	}
	defer unlock()

	fmt.Println("locked")
	// Output: locked
}
