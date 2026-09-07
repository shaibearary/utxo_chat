package rpc

// monitorPage is the dependency-free operator console served at /monitor. It
// is deliberately a single self-contained document with no build step and no
// external requests: the node may be running on a machine with no npm, and the
// page must keep working when the machine is offline.
//
// Messages come first and the form after: the console is read most of the time
// and written to occasionally, so the thing an operator came to look at should
// not be below a form they are not using.
//
// The page offers only wallet-signed publishing. The node can also sign from a
// descriptor over /composemessage, but that means pasting a private key into a
// web form, which is not a habit worth building even on regtest.
const monitorPage = `<!doctype html>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>UTXO Chat monitor</title>
<style>
  :root{color-scheme:light dark;--bg:#f6f7f9;--fg:#18202a;--muted:#687386;--card:#fff;--line:#e5e7eb;--head:#eef1f5;--accent:#1e40af;--accentbg:#dbeafe;--err:#b91c1c;--errbg:#fee2e2;--ok:#166534;--okbg:#dcfce7}
  @media (prefers-color-scheme:dark){:root{--bg:#0f1319;--fg:#e6eaf0;--muted:#98a2b3;--card:#171c24;--line:#2a323d;--head:#1f2630;--accent:#93c5fd;--accentbg:#1e3a5f;--err:#fca5a5;--errbg:#4c1d1d;--ok:#86efac;--okbg:#14532d}}
  body{font:16px/1.5 system-ui,-apple-system,sans-serif;margin:0;padding:2rem 1.25rem;background:var(--bg);color:var(--fg)}
  main{max-width:960px;margin:0 auto}
  h1{margin:0 0 .25rem;font-size:1.5rem}
  h2{font-size:1.05rem;margin:0 0 .75rem}
  .muted{color:var(--muted)}
  .row{display:flex;gap:.5rem;align-items:center;flex-wrap:wrap;margin:.75rem 0}
  .pill{display:inline-block;padding:.25rem .6rem;border-radius:999px;background:var(--accentbg);color:var(--accent);font-size:.85rem}
  .card{background:var(--card);border:1px solid var(--line);border-radius:10px;padding:1rem;margin-bottom:1.5rem}
  .msg{border:1px solid var(--line);border-radius:8px;padding:.75rem;margin-bottom:.6rem;background:var(--card)}
  .msg.new{animation:flash 1.4s}
  @keyframes flash{0%,100%{background:var(--card)}40%{background:var(--okbg)}}
  .text{white-space:pre-wrap;word-break:break-word;margin:0 0 .5rem;font-size:1.05rem}
  .binary{font-family:ui-monospace,SFMono-Regular,Menlo,monospace;font-size:.85rem;color:var(--muted);word-break:break-all}
  .meta{font-family:ui-monospace,SFMono-Regular,Menlo,monospace;font-size:.78rem;color:var(--muted);word-break:break-all}
  label{display:block;font-size:.85rem;color:var(--muted);margin:.6rem 0 .2rem}
  input,textarea{width:100%;box-sizing:border-box;padding:.5rem;border:1px solid var(--line);border-radius:6px;background:var(--bg);color:var(--fg);font-family:ui-monospace,SFMono-Regular,Menlo,monospace;font-size:.9rem}
  textarea{font-family:inherit;min-height:4.5rem;resize:vertical}
  textarea.mono{font-family:ui-monospace,SFMono-Regular,Menlo,monospace;font-size:.85rem;min-height:3.4rem}
  button{margin-top:.9rem;padding:.55rem 1.1rem;border:0;border-radius:6px;background:var(--accent);color:var(--card);font-size:.95rem;cursor:pointer}
  button[disabled]{opacity:.6;cursor:progress}
  .grid{display:grid;grid-template-columns:1fr 7rem;gap:.6rem}
  .note{padding:.6rem .8rem;border-radius:6px;font-size:.9rem;margin-top:.8rem}
  .note.err{background:var(--errbg);color:var(--err)}
  .note.ok{background:var(--okbg);color:var(--ok)}
  .empty{color:var(--muted);padding:.5rem 0}
</style>
<main>
  <h1>UTXO Chat monitor</h1>
  <p class="muted">Node <span id="host"></span> &middot; peer address <span id="peeraddr">…</span> &middot; chain <span id="chain">…</span></p>
  <div class="row"><span id="count" class="pill">Loading…</span><span id="updated" class="muted"></span></div>

  <h2>Stored messages</h2>
  <div id="view" class="empty">Loading messages…</div>

  <section class="card">
    <h2>Publish a wallet-signed message</h2>
    <p class="muted" style="margin-top:-.4rem;font-size:.9rem">Sign the payload in your own wallet (Sparrow: <em>Tools &rarr; Sign/Verify Message</em>, using the address that owns the UTXO), then paste the signature here. This node never sees a key, so it works on every chain.</p>
    <form id="signed">
      <div class="grid">
        <div>
          <label for="s-txid">UTXO txid</label>
          <input id="s-txid" name="txid" placeholder="64 hex characters" autocomplete="off" spellcheck="false" required>
        </div>
        <div>
          <label for="s-vout">vout</label>
          <input id="s-vout" name="vout" type="number" min="0" step="1" value="0" required>
        </div>
      </div>
      <label for="s-payload">Signed payload &mdash; the <em>Message</em> box in your wallet</label>
      <textarea id="s-payload" name="payload" placeholder="the exact text you signed, e.g. Hello" required></textarea>
      <p class="muted" style="font-size:.82rem;margin:.4rem 0 0">Must match what you signed byte for byte. A stray space changes the digest and the node will reject the signature.</p>
      <label for="s-signature">Wallet signature &mdash; the <em>Signature</em> box in your wallet</label>
      <textarea id="s-signature" name="signature" class="mono" placeholder="long base64 string ending in = , e.g. smpAUF3Jt8Bk1I89e…AQ==" autocomplete="off" spellcheck="false" required></textarea>
      <button type="submit" id="s-send">Publish signed message</button>
      <div id="s-result"></div>
    </form>
  </section>

</main>
<script>
(function () {
  var seen = new Set();
  var first = true;

  function esc(s) {
    return String(s).replace(/[&<>"']/g, function (c) {
      return { '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c];
    });
  }

  function note(el, kind, text) {
    el.innerHTML = '<div class="note ' + kind + '">' + esc(text) + '</div>';
  }

  document.getElementById('host').textContent = location.host;

  function render(list) {
    var view = document.getElementById('view');
    if (!list.length) {
      view.className = 'empty';
      view.textContent = 'No messages currently stored.';
      seen = new Set();
      return;
    }
    view.className = '';
    view.innerHTML = list.map(function (m) {
      // Only flash rows that arrived after the first paint, otherwise every
      // message flashes on page load and the signal is meaningless.
      var isNew = !first && !seen.has(m.outpoint);
      var body = m.is_text
        ? '<p class="text">' + esc(m.payload) + '</p>'
        : '<p class="binary">' + esc(m.payload_hex) + ' <em>(not valid UTF-8)</em></p>';
      return '<div class="msg' + (isNew ? ' new' : '') + '">' + body +
        '<div class="meta">' + esc(m.outpoint) + ' &middot; ' + m.size + ' bytes</div></div>';
    }).join('');
    seen = new Set(list.map(function (m) { return m.outpoint; }));
    first = false;
  }

  async function refresh() {
    try {
      var r = await fetch('getmessages', { cache: 'no-store' });
      if (!r.ok) throw new Error('HTTP ' + r.status);
      var d = await r.json();
      document.getElementById('count').textContent = d.count + ' message' + (d.count === 1 ? '' : 's');
      document.getElementById('updated').textContent = 'Updated ' + new Date().toLocaleTimeString();
      document.getElementById('chain').textContent = d.chain || 'unknown';
      document.getElementById('peeraddr').textContent = d.node || 'unknown';
      render(d.messages || []);
    } catch (e) {
      var view = document.getElementById('view');
      view.className = '';
      note(view, 'err', 'Cannot read node: ' + e.message);
    }
  }

  document.getElementById('signed').addEventListener('submit', async function (ev) {
    ev.preventDefault();
    var button = document.getElementById('s-send');
    var result = document.getElementById('s-result');
    button.disabled = true;
    result.innerHTML = '';
    try {
      // The payload is sent verbatim. Trimming it would change the BIP-322
      // digest the wallet signed and the node would reject a valid signature.
      var r = await fetch('submitsigned', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          txid: document.getElementById('s-txid').value.trim(),
          vout: Number(document.getElementById('s-vout').value),
          // Strip all whitespace, not just the ends: wallets wrap the
          // signature across lines and a copy carries the break along.
          signature: document.getElementById('s-signature').value.replace(/\s+/g, ''),
          payload: document.getElementById('s-payload').value
        })
      });
      var d = await r.json();
      if (!r.ok || !d.success) throw new Error(d.error || ('HTTP ' + r.status));
      note(result, 'ok', 'Published ' + d.outpoint);
      document.getElementById('s-payload').value = '';
      document.getElementById('s-signature').value = '';
      refresh();
    } catch (e) {
      note(result, 'err', e.message);
    } finally {
      button.disabled = false;
    }
  });

  refresh();
  setInterval(refresh, 2000);
})();
</script>
`
