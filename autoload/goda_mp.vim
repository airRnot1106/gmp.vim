let s:root = expand('<sfile>:p:h:h')
let s:previewing = 0

augroup goda_mp_session
augroup END

function! goda_mp#open() abort
  if &filetype !=# 'markdown'
    return s:warn('not a markdown file')
  endif
  call s:forget()
  try
    " goda keeps a failed start until the next goda#register(), and a later
    " :PreviewMarkdown has to see a binary built in the meantime.
    call goda#register('mp', function('s:start'),
          \ #{version: 1, module: 'github.com/airRnot1106/goda-mp.vim'})
    let url = goda#request('mp', 'open', #{
          \ host: s:option('server_host', '127.0.0.1'),
          \ port: s:option('server_port', 0),
          \ root: expand('%:p:h'),
          \ name: expand('%:t'),
          \ lines: getline(1, '$'),
          \ cursor: line('.'),
          \ })
    let s:previewing = 1
    augroup goda_mp_session
      autocmd TextChanged,TextChangedI,TextChangedP <buffer> call s:push('text', getline(1, '$'))
      autocmd CursorMoved,CursorMovedI <buffer> call s:push('cursor', line('.'))
      autocmd BufDelete,BufWipeout <buffer> call goda_mp#close()
    augroup END
    call s:browse(url)
  catch
    call s:warn(v:exception)
  endtry
endfunction

function! goda_mp#close() abort
  call s:forget()
  if goda#is_running('mp')
    call goda#notify('mp', 'close')
  endif
endfunction

function! goda_mp#toggle() abort
  if s:previewing
    call goda_mp#close()
  else
    call goda_mp#open()
  endif
endfunction

function! s:start() abort
  if exists('g:goda_mp#binary')
    call goda#start(g:goda_mp#binary)
    return
  endif
  for bin in [s:root . '/bin/goda-mp', s:root . '/result/bin/goda-mp']
    if executable(bin)
      call goda#start(bin)
      return
    endif
  endfor
  throw 'goda-mp: no binary found, run nix build in ' . s:root . ' or see :help goda-mp-install'
endfunction

" goda#notify() starts a registered binary that died, and the fresh process
" has no session, so the check comes first.
function! s:push(method, arg) abort
  if goda#is_running('mp')
    call goda#notify('mp', a:method, a:arg)
  else
    call s:forget()
    call s:warn('preview lost, see goda#log("mp")')
  endif
endfunction

function! s:forget() abort
  let s:previewing = 0
  autocmd! goda_mp_session
endfunction

function! s:browse(url) abort
  echomsg 'goda-mp: ' . a:url
  let Open = get(g:, 'goda_mp#open_browser_fn', '')
  if empty(Open)
    call goda#notify('mp', 'browse', a:url, get(g:, 'goda_mp#browser', ''))
  else
    call call(Open, [a:url])
  endif
endfunction

function! s:option(name, default) abort
  return get(b:, 'goda_mp_' . a:name, get(g:, 'goda_mp#' . a:name, a:default))
endfunction

function! s:warn(message) abort
  " Neovim wraps an error of the binary in the name of the request.
  let message = substitute(a:message, '^Vim(\a\+):Invoking .\{-} on channel \d\+:\n\|^goda-mp: ', '', '')
  echohl WarningMsg
  echomsg 'goda-mp: ' . message
  echohl None
endfunction
