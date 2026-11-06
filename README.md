# gmp.vim

Markdown preview for Vim and Neovim, in your browser.

gmp.vim is a port of [bufpreview.vim](https://github.com/kat0h/bufpreview.vim) from [denops.vim](https://github.com/vim-denops/denops.vim) to [goda.vim](https://github.com/Shougo/goda.vim). A Go binary replaces Deno, and the page and its assets are built into that binary, so a preview needs no network access.

## Features

- Works on Vim and Neovim.
- Updates the page as you type, and scrolls it to follow the cursor.
- Renders syntax highlighting, YAML front matter, task lists, KaTeX, Mermaid, and PlantUML.
- Loads relative images from the directory of the Markdown file.

PlantUML diagrams load from the public PlantUML server, as they do in bufpreview.vim.

## Requirements

- Vim 9.1 with `+job` and `+channel`, or Neovim 0.11
- [goda.vim](https://github.com/Shougo/goda.vim)
- [Nix](https://nixos.org) with flakes enabled, to build the binary
- A browser

Windows is not supported.

## Install

### With a plugin manager

Install goda.vim and gmp.vim, then run `nix build` in the gmp.vim directory. The plugin finds the binary at `result/bin/gmp`.

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

### With the flake

The default package is a Vim plugin that holds `bin/gmp` and depends on goda.vim. With Home Manager:

```nix
{
  inputs.gmp-vim.url = "github:airRnot1106/gmp.vim";

  # In your Home Manager module:
  programs.neovim.plugins = [
    inputs.gmp-vim.packages.${pkgs.stdenv.hostPlatform.system}.default
  ];
}
```

The flake also provides `gmp` (the binary), `client` (the page and bundle), and `goda-vim`.

## Try it

These commands open a temporary sample file without your configuration and start the preview in your browser:

```sh
nix run github:airRnot1106/gmp.vim         # Neovim
nix run github:airRnot1106/gmp.vim#vim     # Vim
```

In a checkout, use `nix run` and `nix run .#vim`. Arguments go to the editor. The sample file is deleted when the editor exits.

## Usage

These commands exist in Markdown buffers:

| Command | Action |
| --- | --- |
| `:PreviewMarkdown` | Start the preview. Any open preview closes first. |
| `:PreviewMarkdownClose` | Stop the preview. |
| `:PreviewMarkdownToggle` | Start the preview, or stop it if one is open. |

To open the preview whenever you edit Markdown:

```vim
augroup gmp
  autocmd!
  autocmd FileType markdown PreviewMarkdown
augroup END
```

## Configuration

| Variable | Default | Meaning |
| --- | --- | --- |
| `g:gmp_binary` | Unset | Path of the binary. Overrides `bin/gmp` and `result/bin/gmp`. |
| `g:gmp_open_browser_fn` | `''` | Name of a Vim function that receives the URL and opens it. |
| `g:gmp_browser` | `''` | Browser to open. The system default when empty. |
| `b:gmp_server_host`, `g:gmp_server_host` | `'127.0.0.1'` | Address the server listens on. |
| `b:gmp_server_port`, `g:gmp_server_port` | `0` | Port the server listens on. `0` picks a free port. |

The URL always appears in `:messages`. If the browser does not open, read `goda#log('gmp')`.

## Development

```sh
nix develop
(cd client && npm ci && npm run build)
go test ./...
nix flake check
```

`nix flake check` runs the Go tests, the end-to-end tests on real Vim and Neovim, and a check that `go.mod` and the `goda-vim` input pin the same goda.vim revision. Flakes see only files tracked by Git, so run `git add` on new files before `nix build`.

## Special thanks

gmp.vim keeps the browser client and the behavior of [bufpreview.vim](https://github.com/kat0h/bufpreview.vim) by [Kota Kato](https://github.com/kat0h).

bufpreview.vim credits these projects:

- [denops.vim](https://github.com/vim-denops/denops.vim)
- [markdown-preview.nvim](https://github.com/iamcco/markdown-preview.nvim)
- [previm](https://github.com/previm/previm)
- [dps-ghosttext.vim](https://github.com/gamoutatsumi/dps-ghosttext.vim)
- [Vim-jp](https://vim-jp.org/)

## License

MIT

The client uses code from [markdown-it-inject-linenumbers](https://github.com/digitalmoksha/markdown-it-inject-linenumbers).
