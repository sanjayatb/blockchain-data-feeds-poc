const feedSelect = document.getElementById('feedSelect');
const statusSelect = document.getElementById('statusSelect');
const statusWrap = document.getElementById('statusWrap');
const limitInput = document.getElementById('limitInput');
const refreshBtn = document.getElementById('refreshBtn');
const feedHead = document.getElementById('feedHead');
const feedBody = document.getElementById('feedBody');

const txForm = document.getElementById('txForm');
const fromSelect = document.getElementById('fromSelect');
const toInput = document.getElementById('toInput');
const toSelect = document.getElementById('toSelect');
const valueInput = document.getElementById('valueInput');
const gasPriceInput = document.getElementById('gasPriceInput');
const gasLimitInput = document.getElementById('gasLimitInput');
const nonceInput = document.getElementById('nonceInput');
const mineBtn = document.getElementById('mineBtn');
const txResult = document.getElementById('txResult');

const loadTokenBtn = document.getElementById('loadTokenBtn');
const applyTokenBtn = document.getElementById('applyTokenBtn');
const tokenAddress = document.getElementById('tokenAddress');
const tokenResult = document.getElementById('tokenResult');

let eventSource;

function setFeedHeaders(feed) {
  if (feed === 'logs') {
    feedHead.innerHTML = `
      <tr>
        <th>Block</th>
        <th>Tx</th>
        <th>Event</th>
        <th>Contract</th>
        <th>Args</th>
      </tr>`;
  } else {
    feedHead.innerHTML = `
      <tr>
        <th>Status</th>
        <th>Tx</th>
        <th>From</th>
        <th>To</th>
        <th>Nonce</th>
        <th>Value</th>
        <th>Gas</th>
        <th>Seen</th>
        <th>Replaced By</th>
      </tr>`;
  }
}

function renderRows(feed, rows) {
  feedBody.innerHTML = '';
  if (feed === 'logs') {
    rows.forEach(row => {
      const tr = document.createElement('tr');
      tr.innerHTML = `
        <td>${row.block_number}</td>
        <td class="muted">${short(row.tx_hash)}</td>
        <td>${row.event_name}</td>
        <td class="muted">${short(row.contract_address)}</td>
        <td class="muted">${row.args_json}</td>
      `;
      feedBody.appendChild(tr);
    });
  } else {
    rows.forEach(row => {
      const tr = document.createElement('tr');
      tr.innerHTML = `
        <td class="status-${row.status}">${row.status}</td>
        <td class="muted">${short(row.hash || row.tx_hash)}</td>
        <td class="muted">${short(row.from || row.from_addr)}</td>
        <td class="muted">${short(row.to || row.to_addr)}</td>
        <td>${row.nonce}</td>
        <td>${row.value}</td>
        <td>${row.gas}</td>
        <td>${formatTime(row.last_seen_at)}</td>
        <td class="muted">${short(row.replaced_by_tx_hash)}</td>
      `;
      feedBody.appendChild(tr);
    });
  }
}

function fetchFeed() {
  const feed = feedSelect.value;
  const status = statusSelect.value;
  const limit = limitInput.value || 100;
  const url = feed === 'logs'
    ? `/api/logs?limit=${limit}`
    : `/api/mempool?status=${status}&limit=${limit}`;

  fetch(url)
    .then(r => r.json())
    .then(rows => renderRows(feed, rows))
    .catch(err => console.error(err));
}

function connectStream() {
  if (eventSource) eventSource.close();
  const feed = feedSelect.value;
  const status = statusSelect.value;
  const limit = limitInput.value || 100;
  const url = `/api/stream?feed=${feed}&status=${status}&limit=${limit}`;
  eventSource = new EventSource(url);

  eventSource.onmessage = (evt) => {
    try {
      const rows = JSON.parse(evt.data);
      renderRows(feed, rows);
    } catch (e) {
      console.warn('stream parse failed', e);
    }
  };

  eventSource.onerror = () => {
    eventSource.close();
    eventSource = null;
    setTimeout(fetchFeed, 1000);
  };
}

function short(val) {
  if (!val) return '';
  if (val.length <= 12) return val;
  return `${val.slice(0, 6)}...${val.slice(-4)}`;
}

function formatTime(val) {
  if (!val) return '';
  if (typeof val === 'number') return new Date(val * 1000).toISOString();
  if (typeof val === 'string') return val;
  return '';
}

function loadAccounts() {
  fetch('/api/accounts')
    .then(r => r.json())
    .then(accounts => {
      fromSelect.innerHTML = '';
      toSelect.innerHTML = '';
      accounts.forEach(acc => {
        const opt = document.createElement('option');
        opt.value = acc;
        opt.textContent = acc;
        fromSelect.appendChild(opt);
      });
      accounts.forEach(acc => {
        const opt = document.createElement('option');
        opt.value = acc;
        opt.textContent = acc;
        toSelect.appendChild(opt);
      });
      if (accounts.length > 1) {
        toSelect.selectedIndex = 1;
      }
    });
}

txForm.addEventListener('submit', (e) => {
  e.preventDefault();
  txResult.textContent = 'Sending...';
  const toValue = toInput.value || toSelect.value;
  if (!toValue) {
    txResult.textContent = 'Error: to address is required';
    return;
  }
  const payload = {
    from: fromSelect.value,
    to: toValue,
    value_wei: valueInput.value,
    gas_price_wei: gasPriceInput.value,
    gas_limit: gasLimitInput.value ? parseInt(gasLimitInput.value, 10) : 0,
    nonce: nonceInput.value ? parseInt(nonceInput.value, 10) : null
  };
  fetch('/api/tx/send', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(payload)
  })
    .then(r => r.json())
    .then(res => {
      if (res.error) {
        txResult.textContent = `Error: ${res.error}`;
        return;
      }
      txResult.textContent = `Tx hash: ${res.tx_hash}`;
    })
    .catch(err => {
      txResult.textContent = `Error: ${err}`;
    });
});

mineBtn.addEventListener('click', () => {
  fetch('/api/mine', { method: 'POST' })
    .then(r => r.json())
    .then(() => {
      txResult.textContent = 'Mined block';
    })
    .catch(err => {
      txResult.textContent = `Mine error: ${err}`;
    });
});

loadTokenBtn.addEventListener('click', () => {
  tokenResult.textContent = 'Loading latest token...';
  fetch('/api/token/latest')
    .then(r => r.json())
    .then(res => {
      if (res.address) {
        tokenAddress.value = res.address;
        tokenResult.textContent = `Loaded ${res.address}`;
      } else if (res.error) {
        tokenResult.textContent = `Error: ${res.error}`;
      }
    })
    .catch(err => {
      tokenResult.textContent = `Error: ${err}`;
    });
});

applyTokenBtn.addEventListener('click', () => {
  tokenResult.textContent = 'Applying to config...';
  fetch('/api/token/apply', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ address: tokenAddress.value })
  })
    .then(r => r.json())
    .then(res => {
      if (res.error) {
        tokenResult.textContent = `Error: ${res.error}`;
        return;
      }
      tokenResult.textContent = 'Config updated. Start logs indexer to see logs feed.';
    })
    .catch(err => {
      tokenResult.textContent = `Error: ${err}`;
    });
});

feedSelect.addEventListener('change', () => {
  statusWrap.style.display = feedSelect.value === 'logs' ? 'none' : 'flex';
  setFeedHeaders(feedSelect.value);
  connectStream();
});

statusSelect.addEventListener('change', connectStream);
limitInput.addEventListener('change', connectStream);
refreshBtn.addEventListener('click', fetchFeed);

setFeedHeaders(feedSelect.value);
loadAccounts();
connectStream();
