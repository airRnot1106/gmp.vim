" Runs the commands the test writes to $GODA_MP_E2E_DIR/cmd.N and answers in
" res.N. Vim and Neovim share no remote interface that works headless on both.
set encoding=utf-8
set hidden
let &runtimepath = join(map([$GODA_MP_E2E_PLUGIN, $GODA_MP_E2E_GODA], {_, dir -> escape(dir, ',')}) + [&runtimepath], ',')
filetype plugin on

" Keeps doautocmd quiet when the preview has no autocmds left, so that a
" scenario sees only the plugin's own messages.
autocmd TextChanged,TextChangedI,TextChangedP,CursorMoved,CursorMovedI * :

function! CaptureURL(url) abort
  let g:url = a:url
endfunction

let s:step = 0
while 1
  let s:cmd = $GODA_MP_E2E_DIR . '/cmd.' . s:step
  if filereadable(s:cmd)
    try
      let s:answer = ['ok'] + split(execute(readfile(s:cmd)), "\n")
    catch
      let s:answer = ['error', v:exception]
    endtry
    call writefile(s:answer, $GODA_MP_E2E_DIR . '/res.tmp')
    call rename($GODA_MP_E2E_DIR . '/res.tmp', $GODA_MP_E2E_DIR . '/res.' . s:step)
    let s:step += 1
  endif
  sleep 5m
endwhile
