const ws = new WebSocket((location.protocol === 'https:' ? 'wss://' : 'ws://') + location.host + '/ws');
let W = 60, H = 40;
const canvas = document.getElementById('field');
const ctx = canvas.getContext('2d');
const statsEl = document.getElementById('stats');
const agentListEl = document.getElementById('agentList');
let paused = false;
let lastLineage = {};
let selectedAgent = null;
let randomFoodEnabled = true;
let addFoodMode = false;
let plantAgentMode = false;

document.getElementById('toggleFood').onclick = () => {
  randomFoodEnabled = !randomFoodEnabled;
  document.getElementById('toggleFood').innerText = randomFoodEnabled ? 'Disable Random Food' : 'Enable Random Food';
  ws.send(JSON.stringify({ type: 'toggle_random_food', enabled: randomFoodEnabled }));
}

document.getElementById('addFoodBtn').onclick = () => {
  addFoodMode = !addFoodMode;
  if (addFoodMode) { plantAgentMode = false; document.getElementById('plantAgentBtn').innerText = 'Plant Agent (click canvas)'; document.getElementById('addFoodBtn').innerText = 'Adding Food: ON' }
  else { document.getElementById('addFoodBtn').innerText = 'Add Food (click canvas)' }
}

document.getElementById('plantAgentBtn').onclick = () => {
  plantAgentMode = !plantAgentMode;
  const form = document.getElementById('agentForm');
  form.style.display = plantAgentMode ? 'block' : 'none';
  if (plantAgentMode) { addFoodMode = false; document.getElementById('addFoodBtn').innerText = 'Add Food (click canvas)'; document.getElementById('plantAgentBtn').innerText = 'Planting Agent: ON' }
  else { document.getElementById('plantAgentBtn').innerText = 'Plant Agent (click canvas)'; }
}

ws.onmessage = (ev) => {
  const msg = JSON.parse(ev.data);
  if (msg.type === 'config') { W = msg.w; H = msg.h; resize(); return }
  if (msg.type === 'state') renderState(msg);
}

function resize() {
  canvas.width = 900; canvas.height = 600;
}

function renderState(state) {
  if (paused) return;
  const cellW = canvas.width / W;
  const cellH = canvas.height / H;
  ctx.fillStyle = '#071216'; ctx.fillRect(0, 0, canvas.width, canvas.height);
  ctx.strokeStyle = 'rgba(255,255,255,0.03)';
  ctx.lineWidth = 1;
  for (let i = 0; i <= W; i++) {
    const x = i * cellW;
    ctx.beginPath(); ctx.moveTo(x, 0); ctx.lineTo(x, canvas.height); ctx.stroke();
  }
  for (let j = 0; j <= H; j++) {
    const y = j * cellH;
    ctx.beginPath(); ctx.moveTo(0, y); ctx.lineTo(canvas.width, y); ctx.stroke();
  }

  // draw clouds (semi-transparent)
  if (state.clouds && state.clouds.length) {
    state.clouds.forEach(c => {
      const cx = (c.x + 0.5) * cellW;
      const cy = (c.y + 0.5) * cellH;
      const r = Math.max(cellW, cellH) * c.r * 0.5;
      ctx.fillStyle = 'rgba(100,150,255,0.06)';
      ctx.beginPath(); ctx.arc(cx, cy, r, 0, Math.PI*2); ctx.fill();
      ctx.strokeStyle = 'rgba(100,150,255,0.08)'; ctx.lineWidth = 1; ctx.beginPath(); ctx.arc(cx, cy, r, 0, Math.PI*2); ctx.stroke();
    });
  }

  state.foods.forEach(f => {
    ctx.fillStyle = '#2ecc71';
    const fx = f.x * cellW + cellW * 0.1;
    const fy = f.y * cellH + cellH * 0.1;
    const fw = cellW * 0.8;
    const fh = cellH * 0.8;
    ctx.fillRect(fx, fy, Math.max(2, fw), Math.max(2, fh));
  });

  state.agents.forEach(a => {
    const energy = a.energy || 10
    const size = Math.max(4, Math.min(cellW, cellH) * 0.9 * Math.min(1, energy / 60))
    const sx = a.x * cellW + (cellW - size) / 2;
    const sy = a.y * cellH + (cellH - size) / 2;
    ctx.fillStyle = (a.sex === 'M') ? '#4285f4' : '#db4437';
    ctx.fillRect(sx, sy, size, size);
  });

  lastLineage = state.lineage || {};
  state.agents.sort((x, y) => x.id - y.id)
  const tbody = document.querySelector('#agentTable tbody');
  tbody.innerHTML = '';
  state.agents.slice(0, 500).forEach(a => {
    const tr = document.createElement('tr');
    tr.innerHTML = `<td>${a.id}</td><td>${(a.energy || 0).toFixed(1)}</td><td>${a.age || 0}</td><td>${a.sex || ''}</td><td>${(a.strength || 0).toFixed(2)}</td><td>${(a.agg || 0).toFixed(2)}</td><td>${(a.repro || 0).toFixed(2)}</td>`;
    tr.onclick = () => { selectedAgent = a.id; renderAgentDetails(a, state); };
    tbody.appendChild(tr);
  });

  statsEl.innerText = `Population: ${state.metrics.population}  Avg energy: ${state.metrics.avg_energy.toFixed(2)}  Births:${state.metrics.births || 0} Deaths:${state.metrics.deaths || 0} Avg life:${(state.metrics.avg_life || 0).toFixed(1)}`;

  const eventLog = document.getElementById('eventLog');
  const eventCount = document.getElementById('eventCount');
  if (state.events && state.events.length > 0) {
    if (eventCount) {
      eventCount.innerText = `(${state.events.length})`;
    }
    let html = '';
    const colorMap = {
      'kill': '#ff6b6b',
      'death': '#c92a2a',
      'birth': '#51cf66',
      'eat': '#ffd43b',
      'attack': '#ff922b',
      'merge': '#da77f2'
    };
    const displayLimit = 100;
    const startIdx = Math.max(0, state.events.length - displayLimit);
    for (let i = state.events.length - 1; i >= startIdx; i--) {
      const e = state.events[i];
      const color = colorMap[e.type] || '#888';
      html += `<div style="color:${color}">⚡ ${e.message}</div>`;
    }
    eventLog.innerHTML = html;
  }
}

function renderGenealogy(id, state) {
  const g = document.getElementById('genealogy');
  g.innerHTML = '';
  const agents = {};
  state.agents.forEach(a => { agents[a.id] = a; });
  const lineage = state.lineage || {};
  const buildParents = (cid, depth) => {
    if (depth <= 0) return '';
    const ps = lineage[cid] || [];
    if (ps.length === 0) return '';
    let html = '<ul>';
    ps.forEach(p => {
      const pa = agents[p];
      const label = pa ? `#${p} e:${(pa.energy || 0).toFixed(1)} a:${pa.age || 0}` : `#${p}`;
      html += `<li>${label}${buildParents(p, depth - 1)}</li>`;
    });
    html += '</ul>';
    return html;
  };
  const buildChildren = (cid, depth) => {
    if (depth <= 0) return '';
    const children = Object.keys(lineage).filter(k => (lineage[k] || []).includes(cid)).map(Number);
    if (children.length === 0) return '';
    let html = '<ul>';
    children.forEach(cid2 => {
      const ca = agents[cid2];
      const label = ca ? `#${cid2} e:${(ca.energy || 0).toFixed(1)} a:${ca.age || 0}` : `#${cid2}`;
      html += `<li>${label}${buildChildren(cid2, depth - 1)}</li>`;
    });
    html += '</ul>';
    return html;
  };
  const root = agents[id];
  const title = document.createElement('div');
  title.innerHTML = `<strong>Agent #${id}</strong> ${root ? `e:${root.energy.toFixed(1)} age:${root.age || 0}` : ''}`;
  g.appendChild(title);
  const parentsHtml = buildParents(id, 3);
  if (parentsHtml) {
    const pdiv = document.createElement('div');
    pdiv.innerHTML = `<strong>Ancestors</strong>` + parentsHtml;
    g.appendChild(pdiv);
  }
  const childrenHtml = buildChildren(id, 3);
  if (childrenHtml) {
    const cdiv = document.createElement('div');
    cdiv.innerHTML = `<strong>Descendants</strong>` + childrenHtml;
    g.appendChild(cdiv);
  }
}

function renderAgentDetails(agent, state) {
  const g = document.getElementById('genealogy');
  g.innerHTML = '';
  const title = document.createElement('div');
  title.innerHTML = `<strong>Agent #${agent.id}</strong> <span style="font-size:12px;color:#cfe6ff">energy:${agent.energy.toFixed(1)} age:${agent.age || 0}</span>`;
  g.appendChild(title);
  const list = document.createElement('div');
  list.className = 'geneTree';
  let html = '<ul>';
  html += `<li><strong>Sex:</strong> ${agent.sex}</li>`;
  html += `<li><strong>Speed:</strong> ${agent.spd}</li>`;
  html += `<li><strong>Strength:</strong> ${agent.strength.toFixed(2)}</li>`;
  html += `<li><strong>Aggression:</strong> ${agent.agg.toFixed(2)}</li>`;
  html += `<li><strong>Repro:</strong> ${agent.repro.toFixed(2)}</li>`;
  html += `<li><strong>Policy Dir:</strong> ${agent.policy_dir}</li>`;
  html += `<li><strong>Parents:</strong> ${(agent.parents || []).join(', ')}</li>`;
  html += `<li><strong>Experience:</strong> ${JSON.stringify(agent.exp || {})}</li>`;
  html += '</ul>';
  list.innerHTML = html;
  g.appendChild(list);
}

document.getElementById('pause').onclick = () => { paused = !paused; document.getElementById('pause').innerText = paused ? 'Resume' : 'Pause'; }

window.addEventListener('resize', resize);
resize();

canvas.addEventListener('click', (ev) => {
  const rect = canvas.getBoundingClientRect();
  const x = ev.clientX - rect.left;
  const y = ev.clientY - rect.top;
  const cellW = canvas.width / W;
  const cellH = canvas.height / H;
  const gx = Math.floor(x / cellW);
  const gy = Math.floor(y / cellH);
  if (addFoodMode) {
    ws.send(JSON.stringify({ type: 'add_food', x: gx, y: gy, energy: 12 }));
    return;
  }
  if (plantAgentMode) {
    const energy = parseFloat(document.getElementById('ag_energy').value) || 40;
    const sex = document.getElementById('ag_sex').value || 'M';
    const agg = parseFloat(document.getElementById('ag_agg').value) || 0.5;
    const spd = parseInt(document.getElementById('ag_spd').value) || 1;
    const str = parseFloat(document.getElementById('ag_str').value) || 5.0;
    const repro = parseFloat(document.getElementById('ag_repro').value) || 0.05;
    ws.send(JSON.stringify({ type: 'add_agent', x: gx, y: gy, energy: energy, sex: sex, agg: agg, spd: spd, strength: str, repro: repro }));
    return;
  }
});
