# gmp.vim

Markdown preview for Vim and Neovim, right in your browser.

gmp.vim is a port of [bufpreview.vim](https://github.com/kat0h/bufpreview.vim) from [denops.vim](https://github.com/vim-denops/denops.vim) to [goda.vim](https://github.com/Shougo/goda.vim). It replaces Deno with a Go binary that bundles the preview page and its assets directly, so you can preview files without network access.

## Features

- Works in Vim and Neovim
- Live preview updates as you type and follows your cursor
- Syntax highlighting, YAML front matter, task lists, KaTeX, Mermaid, and PlantUML support
- Relative images resolve from the Markdown file's directory

PlantUML diagrams are fetched from the public PlantUML server, matching bufpreview.vim's behavior.

## Requirements

- Vim 9.1 with `+job` and `+channel`, or Neovim 0.11
- [goda.vim](https://github.com/Shougo/goda.vim)
- [Nix](https://nixos.org) with flakes enabled, to build the binary
- A browser

Windows is not supported.

## Installation

### Using a plugin manager

Install both goda.vim and gmp.vim, then run `nix build` inside the plugin directory. The plugin expects the binary at `result/bin/gmp`.

With [lazy.nvim](https://github.com/folke/lazy.nvim):

```lua
{
  'airRnot1106/gmp.vim',
  dependencies = { 'Shougo/goda.vim' },
  build = 'nix build',
}
```

With [vim-plug](https://github.com/junegunn/vim-plug):

```vim
Plug 'Shougo/goda.vim'
Plug 'airRnot1106/gmp.vim', { 'do': 'nix build' }
```

With [dein.vim](https://github.com/Shougo/dein.vim):

```vim
call dein#add('Shougo/goda.vim')
call dein#add('airRnot1106/gmp.vim', {'build': 'nix build'})
```

### Using Nix Flakes

The default package provides the Vim plugin along with `bin/gmp`, and handles the goda.vim dependency automatically. For Home Manager:

```nix
{
  inputs.gmp-vim.url = "github:airRnot1106/gmp.vim";

  # In your Home Manager module:
  programs.neovim.plugins = [
    inputs.gmp-vim.packages.${pkgs.stdenv.hostPlatform.system}.default
  ];
}
```

The flake also outputs `gmp` (the binary), `client` (the frontend bundle), and `goda-vim`.

## Quick Start

You can test the preview without touching your config by running:

```sh
nix run github:airRnot1106/gmp.vim         # Neovim
nix run github:airRnot1106/gmp.vim#vim     # Vim
```

If you already cloned the repo locally, use `nix run` or `nix run .#vim` instead. Extra arguments are passed through to the editor, and the temporary sample file is cleaned up on exit.

## Usage

The following commands are available when editing Markdown:

| Command | Action |
| --- | --- |
| `:PreviewMarkdown` | Start previewing the current buffer, closing any active preview first. |
| `:PreviewMarkdownClose` | Close the active preview. |
| `:PreviewMarkdownToggle` | Toggle the preview on or off. |

To automatically open a preview when opening a Markdown file:

```vim
augroup gmp
  autocmd!
  autocmd FileType markdown PreviewMarkdown
augroup END
```

## Options

| Variable | Default | Description |
| --- | --- | --- |
| `g:gmp_binary` | Unset | Path to the binary. Overrides `bin/gmp` and `result/bin/gmp`. |
| `g:gmp_open_browser_fn` | `''` | Custom Vim function name to open the preview URL. |
| `g:gmp_browser` | `''` | Browser command to use (defaults to system default). |
| `b:gmp_server_host`, `g:gmp_server_host` | `'127.0.0.1'` | Server listen address. |
| `b:gmp_server_port`, `g:gmp_server_port` | `0` | Server port (0 chooses an available port). |

The preview URL is printed to `:messages`. If your browser does not open automatically, check `goda#log('gmp')`.

## Development

```sh
nix develop
(cd client && npm ci && npm run build)
go test ./...
nix flake check
```

Running `nix flake check` will run the Go tests, E2E tests against actual Vim and Neovim instances, and make sure `go.mod` stays in sync with the `goda-vim` flake input. Keep in mind that Nix flakes ignore untracked files, so remember to `git add` any new files before testing or building.

## Special thanks

- [bufpreview.vim](https://github.com/kat0h/bufpreview.vim)

## License

MIT

The client uses code from [markdown-it-inject-linenumbers](https://github.com/digitalmoksha/markdown-it-inject-linenumbers).
