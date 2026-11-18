// LoRa apiary dashboard — vanilla JS, no external dependencies.
// Renders node map, channel occupancy timeline, SF distribution, loss reasons
// and battery estimates for both the fixed-SF and ADR simulation reports.

const SF_COLORS = { 7: '#37d399', 8: '#7ed957', 9: '#ffc24b', 10: '#ff9f43', 11: '#ff7a59', 12: '#ff5d73' };
const LOSS_LABELS = {
  below_sensitivity: '低于接收灵敏度',
  capture_lost: '同频冲突捕获失败',
  inter_sf_interference: '异 SF 干扰(准正交破坏)',
  demodulator_full: '网关解调路数已满',
  downlink_interfered: '下行被干扰',
  rx_window_missed: '错过接收窗口',
};

let DATA = null;
let mode = 'fixed';
let selectedChannel = 0;
let selectedNode = null;

const $ = (id) => document.getElementById(id);
const report = () => (mode === 'fixed' ? DATA.fixed : DATA.adr);
const pct = (x, d = 2) => (x * 100).toFixed(d) + '%';
const fixed = (x, d = 1) => Number.isFinite(x) ? x.toFixed(d) : '—';

async function load(init) {
  const resp = await fetch('/api/comparison');
  DATA = await resp.json();
  if (init) bindUI();
  renderAll();
}

function bindUI() {
  document.querySelectorAll('.mode-btn').forEach((btn) => {
    btn.addEventListener('click', () => {
      mode = btn.dataset.mode;
      document.querySelectorAll('.mode-btn').forEach((b) => b.classList.toggle('active', b === btn));
      selectedNode = null;
      renderAll();
    });
  });
  $('rerun').addEventListener('click', async () => {
    const btn = $('rerun');
    btn.disabled = true; btn.textContent = '仿真中…';
    const body = {
      seed: parseInt($('seed').value, 10),
      nodes: parseInt($('nodes').value, 10),
      hours: parseInt($('hours').value, 10),
    };
    const resp = await fetch('/api/rerun', {
      method: 'POST', headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(body),
    });
    DATA = await resp.json();
    selectedNode = null;
    btn.disabled = false; btn.textContent = '重新仿真';
    renderAll();
  });
}

function renderAll() {
  const r = report();
  $('meta').textContent =
    `种子 ${DATA.scenarioSeed} · ${DATA.nodeCount} 个节点 · 仿真 ${r.aggregate.durationSec / 3600} 小时 · 模式 ${mode === 'fixed' ? '固定扩频因子' : '自适应速率 ADR'}`;
  renderKPI();
  renderMap();
  renderSFChart();
  renderChannelToolbar();
  renderTimeline();
  renderLoss();
  renderBattery();
  renderCompare();
  renderDetBadge();
}

function renderKPI() {
  const a = report().aggregate;
  const f = DATA.fixed.aggregate;
  const delta = (cur, base, unit = 'pt', invertGood = false) => {
    const d = (cur - base) * (unit === 'pt' ? 100 : 1);
    const cls = Math.abs(d) < 1e-9 ? 'flat' : ((d > 0) !== invertGood ? 'up' : 'down');
    const sign = d > 0 ? '+' : '';
    return `<span class="${cls}">${sign}${d.toFixed(1)} ${unit}</span>`;
  };
  const kpis = [
    ['网络投递率', pct(a.networkDeliveryRate), delta(a.networkDeliveryRate, f.networkDeliveryRate)],
    ['节点 PDR 中位数', pct(a.medianNodePdr), delta(a.medianNodePdr, f.medianNodePdr)],
    ['尝试上行帧', a.ulAttempted.toLocaleString(), `<span class="flat">阻塞 ${a.dcBlocked}</span>`],
    ['成功下行', a.ulAcked.toLocaleString(), `<span class="flat">共调度 ${a.dlCount}</span>`],
    ['电池寿命中位数(天)', fixed(a.medianBatteryDays), delta(a.medianBatteryDays, f.medianBatteryDays, '天')],
    ['平均信道占用', pct(a.channelOverallMean), delta(a.channelOverallMean, f.channelOverallMean)],
  ];
  $('kpi-row').innerHTML = kpis.map(([label, value, sub]) =>
    `<div class="kpi"><div class="label">${label}</div><div class="value">${value}</div><div class="sub">${sub}</div></div>`
  ).join('');
}

// ---- node map -------------------------------------------------------------
function renderMap() {
  const cv = $('map');
  const ctx = cv.getContext('2d');
  const W = cv.width, H = cv.height, pad = 28;
  ctx.clearRect(0, 0, W, H);
  const r = report();
  const maxR = Math.max(...r.nodes.map((n) => Math.hypot(n.x, n.y)));
  const scale = Math.min(W, H) / 2 - pad;
  const px = (n) => W / 2 + (n.x / maxR) * scale;
  const py = (n) => H / 2 + (n.y / maxR) * scale;

  // range rings
  ctx.strokeStyle = 'rgba(138,160,196,.18)';
  ctx.fillStyle = 'rgba(138,160,196,.45)';
  ctx.lineWidth = 1;
  for (const frac of [0.33, 0.66, 1]) {
    ctx.beginPath(); ctx.arc(W / 2, H / 2, scale * frac, 0, Math.PI * 2); ctx.stroke();
  }
  ctx.font = '10px sans-serif';
  ctx.fillText(`${Math.round(maxR * 0.33)} m`, W / 2 + 4, H / 2 - scale * 0.33);
  ctx.fillText(`${Math.round(maxR)} m`, W / 2 + 4, H / 2 - scale + 2);

  // gateway at origin
  ctx.fillStyle = '#4f9cff';
  ctx.beginPath(); ctx.arc(W / 2, H / 2, 7, 0, Math.PI * 2); ctx.fill();
  ctx.fillStyle = '#cfe0ff';
  ctx.fillText('网关', W / 2 + 10, H / 2 + 4);

  // nodes
  for (const n of r.nodes) {
    const x = px(n), y = py(n);
    ctx.beginPath();
    ctx.arc(x, y, selectedNode === n.id ? 6.5 : 4, 0, Math.PI * 2);
    ctx.fillStyle = SF_COLORS[n.currentSF];
    ctx.globalAlpha = n.deliveryRate < 0.8 ? 0.55 : 0.92;
    ctx.fill();
    ctx.globalAlpha = 1;
    if (selectedNode === n.id) {
      ctx.strokeStyle = '#fff'; ctx.lineWidth = 1.5; ctx.stroke();
    }
  }

  // SF legend
  let lx = 10;
  ctx.font = '11px sans-serif';
  for (let sf = 7; sf <= 12; sf++) {
    ctx.fillStyle = SF_COLORS[sf];
    ctx.beginPath(); ctx.arc(lx + 5, H - 14, 5, 0, Math.PI * 2); ctx.fill();
    ctx.fillStyle = '#8aa0c4';
    ctx.fillText('SF' + sf, lx + 13, H - 10);
    lx += 52;
  }

  cv.onclick = (ev) => {
    const rect = cv.getBoundingClientRect();
    const mx = (ev.clientX - rect.left) * (W / rect.width);
    const my = (ev.clientY - rect.top) * (H / rect.height);
    let best = null, bestD = 12;
    for (const n of r.nodes) {
      const d = Math.hypot(px(n) - mx, py(n) - my);
      if (d < bestD) { bestD = d; best = n; }
    }
    selectedNode = best ? best.id : null;
    renderMap();
    renderNodeDetail();
  };
  renderNodeDetail();
}

function renderNodeDetail() {
  const el = $('node-detail');
  const r = report();
  if (selectedNode === null) {
    el.innerHTML = '点击地图上的节点查看链路与电池详情';
    return;
  }
  const n = r.nodes.find((x) => x.id === selectedNode);
  const hist = Object.entries(n.sfHistogram).sort((a, b) => a[0] - b[0])
    .map(([sf, c]) => `SF${sf}:${c}`).join(' · ');
  const losses = Object.entries(n.lossReasons).map(([k, v]) => `${LOSS_LABELS[k] || k} ${v}`).join(' · ') || '无';
  el.innerHTML =
    `<b>节点 #${n.id}</b> · 距网关 ${Math.round(n.distance)} m · 坐标 (${Math.round(n.x)}, ${Math.round(n.y)})<br>` +
    `当前 <b>SF${n.currentSF}</b> / 发射 ${n.currentPower} dBm · 上报周期 ${n.reportPeriodSec / 60} 分钟 · 投递率 <b>${pct(n.deliveryRate)}</b> ` +
    `(收 ${n.ulReceived}/${n.ulSent}) · 占空比阻塞 ${n.dcBlocked}<br>` +
    `SF 历史帧: ${hist || '—'}<br>丢包: ${losses}<br>` +
    `已耗电 ${fixed(n.batteryUsedMah, 2)} mAh · 预估电池寿命 <b>${fixed(n.batteryLifeDays)} 天</b> (约 ${fixed(n.batteryLifeDays / 365, 1)} 年)`;
}

// ---- SF distribution ------------------------------------------------------
function renderSFChart() {
  const dist = report().aggregate.sfDistribution;
  const max = Math.max(1, ...Object.values(dist));
  $('sf-chart').innerHTML = [7, 8, 9, 10, 11, 12].map((sf) => {
    const v = dist['SF' + sf] || 0;
    return `<div class="bar-row">
      <span class="sf-name" style="color:${SF_COLORS[sf]}">SF${sf}</span>
      <div class="bar-track"><div class="bar-fill" style="width:${(v / max) * 100}%;background:${SF_COLORS[sf]}"></div></div>
      <span class="bar-val">${v.toLocaleString()}</span>
    </div>`;
  }).join('');
}

// ---- channel timeline -----------------------------------------------------
function renderChannelToolbar() {
  const tb = $('channel-toolbar');
  const r = report();
  tb.innerHTML = r.channels.filter((ch) => ch.id < 8).map((ch) =>
    `<button class="chip ${ch.id === selectedChannel ? 'active' : ''}" data-ch="${ch.id}">
      CH${ch.id} · ${(ch.freqHz / 1e6).toFixed(1)}M · 上限 ${(ch.dutyCycle * 100)}%
    </button>`).join('');
  tb.querySelectorAll('.chip').forEach((c) => c.addEventListener('click', () => {
    selectedChannel = parseInt(c.dataset.ch, 10);
    renderChannelToolbar(); renderTimeline();
  }));
}

function renderTimeline() {
  const cv = $('timeline');
  const ctx = cv.getContext('2d');
  const W = cv.width, H = cv.height, padL = 46, padB = 34, padT = 12, padR = 12;
  ctx.clearRect(0, 0, W, H);
  const ch = report().channels.find((c) => c.id === selectedChannel);
  if (!ch || ch.buckets.length === 0) {
    ctx.fillStyle = '#8aa0c4'; ctx.font = '13px sans-serif';
    ctx.fillText('该信道在仿真窗口内没有流量', padL, H / 2);
    return;
  }
  const maxB = ch.buckets[ch.buckets.length - 1].bucket;
  const x = (b) => padL + (b / Math.max(1, maxB)) * (W - padL - padR);
  const maxRatio = Math.max(0.02, ch.peakRatio, ch.dutyCycle * 1.5);
  const y = (ratio) => H - padB - (ratio / maxRatio) * (H - padT - padB);

  // grid + y labels
  ctx.strokeStyle = 'rgba(138,160,196,.15)';
  ctx.fillStyle = '#8aa0c4'; ctx.font = '10px sans-serif';
  for (let g = 0; g <= 4; g++) {
    const ratio = (maxRatio * g) / 4;
    const yy = y(ratio);
    ctx.beginPath(); ctx.moveTo(padL, yy); ctx.lineTo(W - padR, yy); ctx.stroke();
    ctx.fillText((ratio * 100).toFixed(1) + '%', 6, yy + 3);
  }

  // duty-cycle regulatory limit
  const yLim = y(ch.dutyCycle);
  ctx.setLineDash([6, 5]); ctx.strokeStyle = '#ff6b6b'; ctx.lineWidth = 1.4;
  ctx.beginPath(); ctx.moveTo(padL, yLim); ctx.lineTo(W - padR, yLim); ctx.stroke();
  ctx.setLineDash([]);
  ctx.fillStyle = '#ff6b6b';
  ctx.fillText(`法规占空比上限 ${(ch.dutyCycle * 100)}%`, W - padR - 150, yLim - 5);

  // occupancy bars
  const bw = Math.max(2, (W - padL - padR) / Math.max(1, maxB + 1) - 1);
  for (const b of ch.buckets) {
    const bh = H - padB - y(b.occupancyRatio);
    ctx.fillStyle = b.occupancyRatio > ch.dutyCycle ? '#ff6b6b' : '#4f9cff';
    ctx.fillRect(x(b.bucket), y(b.occupancyRatio), bw, Math.max(1, bh));
  }

  // x axis labels (minutes)
  ctx.fillStyle = '#8aa0c4';
  const ticks = 6;
  for (let i = 0; i <= ticks; i++) {
    const b = Math.round((maxB * i) / ticks);
    ctx.fillText(`${b} min`, x(b) - 12, H - 12);
  }
  ctx.fillStyle = '#cfe0ff';
  ctx.fillText(`峰值占用 ${(ch.peakRatio * 100).toFixed(2)}% · 均值 ${(ch.meanRatio * 100).toFixed(2)}%`, padL, 14);
}

// ---- loss reasons ---------------------------------------------------------
function renderLoss() {
  const losses = report().aggregate.lossReasons;
  const entries = Object.entries(losses).sort((a, b) => b[1] - a[1]);
  const total = entries.reduce((s, [, v]) => s + v, 0);
  const el = $('loss-chart');
  if (total === 0) { el.innerHTML = '<div class="loss-empty">✓ 本模式没有丢包</div>'; return; }
  el.innerHTML = entries.map(([k, v]) => {
    const share = (v / total) * 100;
    return `<div class="loss-row">
      <div class="loss-top"><span>${LOSS_LABELS[k] || k}</span><span>${v} · ${share.toFixed(1)}%</span></div>
      <div class="loss-track"><div class="loss-fill" style="width:${share}%"></div></div>
    </div>`;
  }).join('');
}

// ---- battery --------------------------------------------------------------
function renderBattery() {
  const cv = $('battery');
  const ctx = cv.getContext('2d');
  const W = cv.width, H = cv.height, padL = 50, padB = 34, padT = 14, padR = 16;
  ctx.clearRect(0, 0, W, H);
  const nodes = report().nodes;
  const days = nodes.map((n) => n.batteryLifeDays).filter(Number.isFinite);
  const maxD = Math.max(...days) * 1.05;
  const bins = 24;
  const counts = new Array(bins).fill(0);
  for (const d of days) counts[Math.min(bins - 1, Math.floor((d / maxD) * bins))]++;
  const maxC = Math.max(...counts, 1);
  const bw = (W - padL - padR) / bins;

  ctx.strokeStyle = 'rgba(138,160,196,.15)';
  ctx.fillStyle = '#8aa0c4'; ctx.font = '10px sans-serif';
  for (let g = 0; g <= 4; g++) {
    const yy = H - padB - (g / 4) * (H - padT - padB);
    ctx.beginPath(); ctx.moveTo(padL, yy); ctx.lineTo(W - padR, yy); ctx.stroke();
    ctx.fillText(Math.round((maxC * g) / 4) + ' 节点', 8, yy + 3);
  }
  for (let i = 0; i < bins; i++) {
    const bh = (counts[i] / maxC) * (H - padT - padB);
    ctx.fillStyle = mode === 'adr' ? '#37d399' : '#4f9cff';
    ctx.fillRect(padL + i * bw + 1, H - padB - bh, bw - 2, bh);
    if (i % 4 === 0) {
      ctx.fillStyle = '#8aa0c4';
      ctx.fillText(Math.round((maxD * i) / bins) + 'd', padL + i * bw, H - 12);
    }
  }
  ctx.fillStyle = '#cfe0ff';
  ctx.fillText('电池寿命分布（天）', padL, 12);

  const a = report().aggregate;
  $('battery-stats').innerHTML = `
    <span>中位数 <b>${fixed(a.medianBatteryDays)} 天</b></span>
    <span>平均值 <b>${fixed(a.meanBatteryDays)} 天</b></span>
    <span>总耗电(仿真期) <b>${fixed(a.batteryTotalMah)} mAh</b></span>
    <span>电池容量 <b>8000 mAh</b></span>`;
}

// ---- comparison table -----------------------------------------------------
function renderCompare() {
  const f = DATA.fixed.aggregate, a = DATA.adr.aggregate;
  const cls = (good) => good ? 'up' : 'down';
  $('compare-table').innerHTML = `
    <tr><th>指标</th><th>固定 SF</th><th>ADR</th><th>差值</th></tr>
    <tr><td>网络投递率</td><td>${pct(f.networkDeliveryRate)}</td><td>${pct(a.networkDeliveryRate)}</td>
      <td class="${a.networkDeliveryRate >= f.networkDeliveryRate ? 'up' : 'down'}">${(DATA.deliveryDeltaPct).toFixed(2)} pt</td></tr>
    <tr><td>节点 PDR 中位数</td><td>${pct(f.medianNodePdr)}</td><td>${pct(a.medianNodePdr)}</td>
      <td class="${a.medianNodePdr >= f.medianNodePdr ? 'up' : 'down'}">${((a.medianNodePdr - f.medianNodePdr) * 100).toFixed(2)} pt</td></tr>
    <tr><td>占空比阻塞帧</td><td>${f.dcBlocked}</td><td>${a.dcBlocked}</td>
      <td class="${a.dcBlocked <= f.dcBlocked ? 'up' : 'down'}">${a.dcBlocked - f.dcBlocked}</td></tr>
    <tr><td>解调路满丢包</td><td>${f.lossReasons.demodulator_full || 0}</td><td>${a.lossReasons.demodulator_full || 0}</td>
      <td class="${(a.lossReasons.demodulator_full || 0) <= (f.lossReasons.demodulator_full || 0) ? 'up' : 'down'}">${(a.lossReasons.demodulator_full || 0) - (f.lossReasons.demodulator_full || 0)}</td></tr>
    <tr><td>电池寿命中位数(天)</td><td>${fixed(f.medianBatteryDays)}</td><td>${fixed(a.medianBatteryDays)}</td>
      <td class="${a.medianBatteryDays >= f.medianBatteryDays ? 'up' : 'down'}">${DATA.batteryDeltaDays.toFixed(1)}</td></tr>
    <tr><td>平均信道占用</td><td>${pct(f.channelOverallMean)}</td><td>${pct(a.channelOverallMean)}</td>
      <td class="${a.channelOverallMean <= f.channelOverallMean ? 'up' : 'down'}">${((a.channelOverallMean - f.channelOverallMean) * 100).toFixed(2)} pt</td></tr>`;
}

function renderDetBadge() {
  const el = $('det-badge');
  const ok = DATA.determinism.fixedRepeatedIdentical && DATA.determinism.adrRepeatedIdentical;
  el.className = 'badge' + (ok ? '' : ' fail');
  el.textContent = ok ? '✓ 确定性校验通过：同种子两次运行字节级一致' : '✗ 确定性校验失败';
}

load(true).catch((err) => {
  $('meta').textContent = '加载失败: ' + err;
});
