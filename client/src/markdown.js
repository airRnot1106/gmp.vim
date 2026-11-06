import "github-markdown-css/github-markdown.css";
import "highlight.js/styles/xcode.css";
import "markdown-it-texmath/css/texmath.css";
import "katex/dist/katex.min.css";

import MarkdownIt from "markdown-it";
import HighlightJs from "highlight.js";
import KaTeX from "katex";
import TexMath from "markdown-it-texmath";
import TaskList from "markdown-it-task-lists";
import * as IncrementalDOM from "incremental-dom";
import MarkdownItIncrementalDOM from "markdown-it-incremental-dom";
import MarkdownItMeta from "markdown-it-meta";
import MarkdownItPlantuml from "markdown-it-plantuml";
import mermaid from "mermaid";

mermaid.initialize({ startOnLoad: false });
const mermaidCache = new Map();

const md = new MarkdownIt({
  html: true,
  linkify: true,
  highlight: function (str, lang) {
    if (lang && HighlightJs.getLanguage(lang)) {
      try {
        return HighlightJs.highlight(str, { language: lang }).value;
      } catch (__) {
        console.log("err highlight.js");
      }
    }
    return "";
  },
});
md.use(MarkdownItIncrementalDOM, IncrementalDOM);
md.use(MarkdownItPlantuml);
md.use(TexMath, {
  engine: KaTeX,
  delimiters: "dollars",
  katexOptions: { macros: { "\\RR": "\\mathbb{R}" } },
});
function inject_linenumbers_plugin(md) {
  function injectLineNumbers(tokens, idx, options, env, slf) {
    let line;
    if (tokens[idx].map) {
      line = tokens[idx].map[0] + 1;
      tokens[idx].attrJoin("id", `source-line-${String(line)}`);
    }
    return slf.renderToken(tokens, idx, options, env, slf);
  }
  md.renderer.rules.paragraph_open = injectLineNumbers;
  md.renderer.rules.heading_open = injectLineNumbers;
  md.renderer.rules.list_item_open = injectLineNumbers;
  md.renderer.rules.table_open = injectLineNumbers;
}
md.use(inject_linenumbers_plugin);
md.use(MarkdownItMeta);
md.use(TaskList, { enabled: true });
const defaultRender = md.renderer.rules.link_open ||
  function (tokens, idx, options, _, self) {
    return self.renderToken(tokens, idx, options);
  };
md.renderer.rules.link_open = function (tokens, idx, options, env, self) {
  const aIndex = tokens[idx].attrIndex("target");
  if (tokens[idx]["attrs"][0][1].match("http")) {
    if (aIndex < 0) {
      tokens[idx].attrPush(["target", "_blank"]);
    } else {
      tokens[idx].attrs[aIndex][1] = "_blank";
    }
  }
  return defaultRender(tokens, idx, options, env, self);
};

function closeWindow() {
  window.open("about:blank", "_self").close();
  document.getElementById("render").innerHTML =
    '<h1 align="center">Connection closed</h1>';
  document.title = "Connection closed";
}

function sb(res) {
  let min = res.cursorLine.linePos;
  let e;
  while (true) {
    e = document.getElementById(`source-line-${min}`);
    if (e == null) {
      min = min - 1;
    } else {
      break;
    }
    if (min < 1) {
      break;
    }
  }
  let max = res.cursorLine.linePos;
  while (true) {
    e = document.getElementById(`source-line-${max}`);
    if (e == null) {
      max = max + 1;
    } else {
      break;
    }
    if (max >= res.cursorLine.bufLength) {
      break;
    }
  }
  return [min, max];
}

function getOfs(l, res) {
  if (l == 0) {
    return 0;
  }
  if (l >= res.cursorLine.bufLength) {
    return document.getElementById("render").clientHeight;
  }
  return document.getElementById(`source-line-${l}`).getBoundingClientRect()
    .top + window.pageYOffset;
}

function make_table(data) {
  var isObject = function (o) {
    return (o instanceof Object && !(o instanceof Array)) ? true : false;
  };
  if (isObject(data)) {
    var keys = document.createElement("tr");
    var values = document.createElement("tr");
    for (const key in data) {
      var j = document.createElement("th");
      j.appendChild(make_table(key));
      keys.appendChild(j);
      var k = document.createElement("td");
      k.appendChild(make_table(data[key]));
      values.appendChild(k);
    }
    var ret = document.createElement("table");
    var thead = document.createElement("thead");
    thead.appendChild(keys);
    ret.appendChild(thead);
    var tbody = document.createElement("tbody");
    tbody.appendChild(values);
    ret.appendChild(tbody);
    return ret;
  } else if (Array.isArray(data)) {
    var tbl = document.createElement("tr");
    for (const i of data) {
      var j = document.createElement("td");
      j.appendChild(make_table(i));
      tbl.appendChild(j);
    }
    var tbody = document.createElement("tbody");
    tbody.appendChild(tbl);
    var ret = document.createElement("table");
    ret.appendChild(tbody);
    return ret;
  } else {
    return document.createElement("td").appendChild(
      document.createTextNode(data),
    );
  }
}

const socket = new WebSocket("ws://" + location.host + "/_gmp/ws");
socket.onerror = function (_) {
  document.getElementById("render").innerHTML =
    '<h1 align="center">Connection closed</h1>';
  document.title = "Connection closed";
};
socket.onclose = function () {
  closeWindow();
};
socket.addEventListener("message", function (event) {
  let res = JSON.parse(event.data);
  if (res.bufname != undefined) {
    document.title = res.bufname;
  }

  if (res.buf != undefined) {
    IncrementalDOM.patch(
      document.getElementById("render"),
      md.renderToIncrementalDOM(res.buf.join("\n")),
    );
    const metatbl = document.getElementById("metatbl");
    while (metatbl.firstChild) {
      metatbl.removeChild(metatbl.firstChild);
    }
    metatbl.appendChild(make_table(md.meta));

    document.querySelectorAll('code.language-mermaid').forEach(async (el) => {
      const pre = el.parentElement;
      const code = el.textContent;

      if (el.dataset.lastCode === code) {
        return;
      }
      el.dataset.lastCode = code;
      pre.style.display = 'none';

      let container = pre.nextElementSibling;
      if (!container || !container.classList.contains('mermaid-container')) {
        container = document.createElement('div');
        container.className = 'mermaid-container';
        pre.parentNode.insertBefore(container, pre.nextSibling);
      }

      if (mermaidCache.has(code)) {
        container.innerHTML = mermaidCache.get(code);
        return;
      }

      try {
        // Parse first to check syntax and avoid mermaid's default error UI
        await mermaid.parse(code);

        const id = 'mermaid-' + Math.random().toString(36).substr(2, 9);
        const { svg } = await mermaid.render(id, code);
        mermaidCache.set(code, svg);
        if (document.body.contains(container)) {
          container.innerHTML = svg;
        }
      } catch (e) {
        const errorHtml = `<pre style="color: red;">${e}</pre>`;
        mermaidCache.set(code, errorHtml);
        if (document.body.contains(container)) {
          container.innerHTML = errorHtml;
        }
      }
    });
  }

  if (res.cursorLine != undefined) {
    const e = sb(res);
    const c = e[0];
    const d = e[1];
    let pos;
    if (c == d) {
      pos = getOfs(c, res);
    } else {
      const ofs = e.map((l) => getOfs(l, res));
      const a = ofs[0];
      const b = ofs[1];
      pos = a + (res.cursorLine.linePos - c) * ((b - a) / (d - c));
    }
    window.scrollTo({
      left: 0,
      top: pos - document.getElementsByTagName("html")[0].clientHeight / 2,
      behavior: "smooth",
    });
  }

  if (res.connect != undefined && res.connect == "close") {
    closeWindow();
  }
});
