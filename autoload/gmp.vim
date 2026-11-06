let s:root = expand('<sfile>:p:h:h')
let s:previewing = 0

augroup gmp_session
augroup END

function! gmp#open() abort
  if &filetype !=# 'markdown'
    return s:warn('not a markdown file')
  endif
  call s:forget()
  try
    " goda keeps a failed start until the next goda#register(), and a later
    " :PreviewMarkdown has to see a binary built in the meantime.
    call goda#register('gmp', function('s:start'),
          \ #{version: 1, module: 'github.com/airRnot1106/gmp.vim'})
    let url = goda#request('gmp', 'open', #{
          \ host: s:option('gmp_server_host', '127.0.0.1'),
          \ port: s:option('gmp_server_port', 0),
          \ root: expand('%:p:h'),
          \ name: expand('%:t'),
          \ lines: getline(1, '$'),
          \ cursor: line('.'),
          \ })
    let s:previewing = 1
    augroup gmp_session
      autocmd TextChanged,TextChangedI,TextChangedP <buffer> call s:push('text', getline(1, '$'))
      autocmd CursorMoved,CursorMovedI <buffer> call s:push('cursor', line('.'))
      autocmd BufDelete,BufWipeout <buffer> call gmp#close()
    augroup END
    call s:browse(url)
  catch
    call s:warn(v:exception)
  endtry
endfunction

function! gmp#close() abort
  call s:forget()
  if goda#is_running('gmp')
    call goda#notify('gmp', 'close')
  endif
endfunction

function! gmp#toggle() abort
  if s:previewing
    call gmp#close()
  else
    call gmp#open()
  endif
endfunction

function! s:start() abort
  if exists('g:gmp_binary')
    call goda#start(g:gmp_binary)
    return
  endif
  for bin in [s:root . '/bin/gmp', s:root . '/result/bin/gmp']
    if executable(bin)
      call goda#start(bin)
      return
    endif
  endfor
  throw 'gmp: no binary found, run nix build in ' . s:root . ' or see :help gmp-install'
endfunction

" goda#notify() starts a registered binary that died, and the fresh process
" has no session, so the check comes first.
function! s:push(method, arg) abort
  if goda#is_running('gmp')
    call goda#notify('gmp', a:method, a:arg)
  else
    call s:forget()
    call s:warn('preview lost, see goda#log("gmp")')
  endif
endfunction

function! s:forget() abort
  let s:previewing = 0
  autocmd! gmp_session
endfunction

function! s:browse(url) abort
  echomsg 'gmp: ' . a:url
  let Open = get(g:, 'gmp_open_browser_fn', '')
  if empty(Open)
    call goda#notify('gmp', 'browse', a:url, get(g:, 'gmp_browser', ''))
  else
    call call(Open, [a:url])
  endif
endfunction

function! s:option(name, default) abort
  return get(b:, a:name, get(g:, a:name, a:default))
endfunction

function! s:warn(message) abort
  " Neovim wraps an error of the binary in the name of the request.
  let message = substitute(a:message, '^Vim(\a\+):Invoking .\{-} on channel \d\+:\n\|^gmp: ', '', '')
  echohl WarningMsg
  echomsg 'gmp: ' . message
  echohl None
endfunction
