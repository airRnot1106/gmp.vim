package main

import (
	"github.com/Shougo/goda.vim/host"

	"github.com/airRnot1106/gmp.vim/client"
	"github.com/airRnot1106/gmp.vim/internal/preview"
)

func main() {
	host.Main(preview.New(client.Dist()))
}
