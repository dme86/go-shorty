function showToast(message, variant='success') {
  const root = document.getElementById('toast-root');
  if (!root) { console.warn('toast-root missing'); return; }
  const el = document.createElement('div');
  const base = 'pointer-events-auto shadow rounded-lg px-4 py-2 text-sm transition-opacity duration-300';
  const palette = variant === 'success'
    ? 'bg-green-600 text-white'
    : variant === 'error' ? 'bg-red-600 text-white' : 'bg-gray-800 text-white';
  el.className = `${base} ${palette} opacity-0`;
  el.textContent = message;
  root.appendChild(el);
  requestAnimationFrame(() => { el.classList.remove('opacity-0'); el.classList.add('opacity-100'); });
  setTimeout(() => {
    el.classList.remove('opacity-100'); el.classList.add('opacity-0');
    setTimeout(() => el.remove(), 300);
  }, 1400);
}

async function api(path, opts={}){
  const res = await fetch(path, {headers:{'Content-Type':'application/json'}, ...opts})
  if(!res.ok) throw new Error(await res.text())
  return res.json()
}

// ------- Age & Date helpers -------
function ageLabel(iso) {
  if (!iso) return '';
  const created = new Date(iso);
  const now = new Date();
  const ms = now - created;
  const h = ms / 36e5;           // Stunden
  const d = ms / 864e5;          // Tage
  const y = ms / (365 * 864e5);  // Jahre

  if (h < 1)   return 'freshly shortened';
  if (h < 6)   return 'a few hours old';
  if (h < 36)  return 'a day old';
  if (d < 7)   return 'a few days old';
  if (d < 21)  return 'couple of weeks old';
  if (y < 1)   return 'months old';
  return 'years old';
}

function formatDateOnly(iso) {
  if (!iso) return '';
  const d = new Date(iso);
  return d.toLocaleDateString(undefined, { weekday:'short', year:'numeric', month:'short', day:'numeric' });
}

// ------- QR Modal -------
function showQR(src, codeLabel='') {
  const modal    = document.getElementById('qr-modal');
  const backdrop = document.getElementById('qr-backdrop');
  const panel    = document.getElementById('qr-panel');
  const img      = document.getElementById('qr-image');
  const lbl      = document.getElementById('qr-code-label');
  const dl       = document.getElementById('qr-download');

  lbl.textContent = codeLabel ? `/${codeLabel}` : '';

  const finalSrc = src + (src.includes('?') ? '&' : '?') + 't=' + Date.now();
  img.src = finalSrc;
  if (dl) { dl.href = finalSrc; dl.setAttribute('download', (codeLabel || 'qr') + '.png'); }

  modal.classList.remove('hidden');
  document.body.classList.add('overflow-hidden');
  requestAnimationFrame(() => {
    backdrop.classList.add('opacity-100');
    panel.classList.remove('opacity-0', 'scale-95');
    panel.classList.add('opacity-100', 'scale-100');
  });

  const close = () => hideQR();
  document.getElementById('qr-close').onclick = close;
  backdrop.onclick = close;
  modal.addEventListener('click', (e) => {
    const r = panel.getBoundingClientRect();
    const inside = e.clientX >= r.left && e.clientX <= r.right && e.clientY >= r.top && e.clientY <= r.bottom;
    if (!inside) close();
  });
  modal.onkeydown = (e) => { if (e.key === 'Escape') close(); };
  modal.tabIndex = -1;
  modal.focus();
}

function hideQR() {
  const modal    = document.getElementById('qr-modal');
  const backdrop = document.getElementById('qr-backdrop');
  const panel    = document.getElementById('qr-panel');

  backdrop.classList.remove('opacity-100');
  panel.classList.remove('opacity-100', 'scale-100');
  panel.classList.add('opacity-0', 'scale-95');

  setTimeout(() => {
    modal.classList.add('hidden');
    document.body.classList.remove('overflow-hidden');
  }, 180);
}

// ------- Preview Modal -------
function showPreview(src, codeLabel='') {
  const modal    = document.getElementById('preview-modal');
  const backdrop = document.getElementById('preview-backdrop');
  const panel    = document.getElementById('preview-panel');
  const iframe   = document.getElementById('preview-iframe');
  const lbl      = document.getElementById('preview-code-label');
  if (!modal || !backdrop || !panel || !iframe) return;

  lbl.textContent = codeLabel ? `/${codeLabel}` : '';
  const finalSrc = src + (src.includes('?') ? '&' : '?') + 't=' + Date.now();
  iframe.src = finalSrc;

  modal.classList.remove('hidden');
  document.body.classList.add('overflow-hidden');
  requestAnimationFrame(() => {
    backdrop.classList.add('opacity-100');
    panel.classList.remove('opacity-0', 'scale-95');
    panel.classList.add('opacity-100', 'scale-100');
  });

  const close = () => hidePreview();
  document.getElementById('preview-close').onclick = close;
  backdrop.onclick = close;
  modal.addEventListener('click', (e) => {
    const r = panel.getBoundingClientRect();
    if (e.clientX < r.left || e.clientX > r.right || e.clientY < r.top || e.clientY > r.bottom) close();
  });
  modal.onkeydown = (e) => { if (e.key === 'Escape') close(); };
  modal.tabIndex = -1;
  modal.focus();
}

function hidePreview() {
  const modal    = document.getElementById('preview-modal');
  const backdrop = document.getElementById('preview-backdrop');
  const panel    = document.getElementById('preview-panel');
  if (!modal || !backdrop || !panel) return;
  backdrop.classList.remove('opacity-100');
  panel.classList.remove('opacity-100', 'scale-100');
  panel.classList.add('opacity-0', 'scale-95');
  setTimeout(() => {
    modal.classList.add('hidden');
    document.body.classList.remove('overflow-hidden');
    const iframe = document.getElementById('preview-iframe');
    if (iframe) iframe.src = 'about:blank';
  }, 180);
}

// ------- List Rendering -------
async function loadLinks(){
  const container = document.getElementById('links')
  container.innerHTML = ''
  const data = await api('/api/links')
  data.forEach(L => {
    const t = document.getElementById('linkCard').content.cloneNode(true)

    // Basics
    t.querySelector('.short').textContent = `${window.BASE_URL}/${L.code}`
    t.querySelector('.short').href = `${window.BASE_URL}/${L.code}`

    // Preview (opens overlay)
    const prevLink = t.querySelector('.preview');
    prevLink.href = `${window.BASE_URL}/preview/${L.code}`
    prevLink.addEventListener('click', (e) => { e.preventDefault(); showPreview(prevLink.href, L.code); });

    // QR (opens QR modal)
    const qrLink = t.querySelector('.qr');
    qrLink.href = `${window.BASE_URL}/api/links/${L.code}/qr.png`
    qrLink.addEventListener('click', (e) => { e.preventDefault(); showQR(qrLink.href, L.code); });

    // Clicks & Age
    t.querySelector('.clicks').textContent = L.click_count
    t.querySelector('.age-chip').textContent = ageLabel(L.created_at);

    // Exp badge (only if expires_at present)
// Exp badge (only if expires_at is actually valid)
const expEl = t.querySelector('.exp-chip');
let expISO = null;

if (L.expires_at) {
  if (typeof L.expires_at === 'string') {
    expISO = L.expires_at;
  } else if (typeof L.expires_at === 'object') {
    const valid = L.expires_at.Valid ?? L.expires_at.valid ?? false;
    if (valid) {
      expISO = L.expires_at.Time ?? L.expires_at.time ?? null;
    }
  }
}

if (expISO && expISO !== '0001-01-01T00:00:00Z') {
  const nice = formatDateOnly(expISO);
  if (nice) {
    expEl.title = 'Expires on ' + nice;
    expEl.classList.remove('hidden');
  } else {
    expEl.classList.add('hidden');
    expEl.removeAttribute('title');
  }
} else {
  expEl.classList.add('hidden');
  expEl.removeAttribute('title');
}


    // Tags
    const tagWrap = t.querySelector('.tags');
    if (tagWrap && Array.isArray(L.tags)) {
      tagWrap.innerHTML = '';
      L.tags.slice(0,6).forEach(tag => {
        const s = document.createElement('span');
        s.className = 'px-2 py-0.5 bg-gray-100 rounded-full text-xs';
        s.textContent = tag;
        tagWrap.appendChild(s);
      });
    }

    // Copy button with toast
    const copyBtn = t.querySelector('.copy');
    copyBtn.addEventListener('click', async (e) => {
      e.preventDefault();
      try {
        await navigator.clipboard.writeText(`${window.BASE_URL}/${L.code}`);
        showToast('Copied!', 'success');
      } catch (err) {
        console.error('Clipboard copy failed:', err);
        showToast('Copy failed', 'error');
      }
    });

    // Small meta line texts
    t.querySelector('.text-sm .font-mono').textContent = `/${L.code}`
    t.querySelector('.text-sm .truncate.inline-block').textContent = L.long_url

    container.appendChild(t)
  })
}

// ------- Page Boot -------
document.addEventListener('DOMContentLoaded', () => {
  const form = document.getElementById('createForm')
  form.addEventListener('submit', async (e) => {
    e.preventDefault()
    try{
      const url = document.getElementById('url').value
      const expiresDateOnly = document.getElementById('expires_at').value // "YYYY-MM-DD" or ""
      const max_clicks = document.getElementById('max_clicks').value

      const body = { url, expires_at: null, max_clicks: max_clicks ? parseInt(max_clicks,10) : null }

      // convert date-only to local 23:59:59 → ISO
      if (expiresDateOnly) {
        const [y,m,d] = expiresDateOnly.split('-').map(Number);
        const endLocal = new Date(y, (m-1), d, 23, 59, 59, 0);
        body.expires_at = endLocal.toISOString();
      }

      const res = await api('/api/links', { method:'POST', body: JSON.stringify(body) })
      await navigator.clipboard.writeText(res.short_url).catch(()=>{})
      showToast('Copied!', 'success')
      await loadLinks()
      form.reset()
    } catch(err){
      showToast(err.message || 'Error', 'error')
    }
  })

  loadLinks().catch(console.error)
})

