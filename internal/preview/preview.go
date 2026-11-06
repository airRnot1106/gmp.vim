// Package preview is the gmp plugin. It relays a buffer to browsers.
package preview

import (
	"io/fs"

	"github.com/Shougo/goda.vim/sdk"
)

// scriptVersion matches the version autoload/gmp.vim passes to goda#register.
const scriptVersion = 1

// Preview is the plugin served to the editor.
type Preview struct {
	client fs.FS
}

// New returns a plugin that serves the browser page and bundle from client.
func New(client fs.FS) *Preview {
	return &Preview{client: client}
}

// Name implements sdk.Plugin.
func (p *Preview) Name() string { return "gmp" }

// ScriptVersion implements sdk.ScriptVersioner.
func (p *Preview) ScriptVersion() int { return scriptVersion }

// Handlers implements sdk.Plugin.
func (p *Preview) Handlers() map[string]sdk.Handler {
	return map[string]sdk.Handler{}
}
