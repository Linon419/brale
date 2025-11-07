const TF_OPTIONS = ["15m", "1h", "4h", "1d", "3d", "7d"];

function fillTimeframes(select) {
  TF_OPTIONS.forEach((tf) => {
    const option = document.createElement("option");
    option.value = tf;
    option.textContent = tf;
    select.appendChild(option);
  });
}

function tsFromInput(value) {
  if (!value) return 0;
  const ms = Date.parse(value);
  return Number.isNaN(ms) ? 0 : ms;
}

function formatTs(ms) {
  if (!ms) return "-";
  const date = new Date(ms);
  return date.toLocaleString();
}

function toLocalInput(date) {
  const offset = date.getTimezoneOffset() * 60000;
  const local = new Date(date.getTime() - offset);
  return local.toISOString().slice(0, 16);
}

async function safeFetch(url, options = {}) {
  const res = await fetch(url, options);
  if (!res.ok) {
    const msg = await res.text();
    throw new Error(msg || res.statusText);
  }
  return res.json();
}

function setMessage(el, text, type = "") {
  el.textContent = text;
  el.className = `message ${type}`;
}

function updateJobsTable(jobs) {
  const body = document.getElementById("jobsBody");
  body.innerHTML = "";
  if (!jobs.length) {
    const row = document.createElement("tr");
    row.innerHTML = `<td colspan="6" class="muted">暂无任务</td>`;
    body.appendChild(row);
    return;
  }
  jobs
    .sort((a, b) => new Date(b.updated_at) - new Date(a.updated_at))
    .forEach((job) => {
      const row = document.createElement("tr");
      const total = job.total || 0;
      const completed = job.completed || 0;
      const percent =
        total > 0 ? ((completed / total) * 100).toFixed(1) + "%" : "-";
      row.innerHTML = `
        <td>${job.id.slice(0, 8)}…</td>
        <td>${job.params.symbol}/${job.params.timeframe}</td>
        <td>${formatTs(job.params.start)} → ${formatTs(job.params.end)}</td>
        <td>${job.status}${job.message ? `<br/><small>${job.message}</small>` : ""}</td>
        <td>${completed}/${total} (${percent})</td>
        <td>${formatTs(new Date(job.updated_at).getTime())}</td>
      `;
      body.appendChild(row);
    });
}

function sanitizeNumber(val) {
  const num = Number(val);
  if (Number.isNaN(num)) {
    return 0;
  }
  return num;
}

function updateCandlesTable(list) {
  const body = document.getElementById("candlesBody");
  body.innerHTML = "";
  if (!list.length) {
    const row = document.createElement("tr");
    row.innerHTML = `<td colspan="6" class="muted">没有数据</td>`;
    body.appendChild(row);
    return;
  }
  list.forEach((candle) => {
    const row = document.createElement("tr");
    const close = sanitizeNumber(candle.close).toFixed(4);
    const high = sanitizeNumber(candle.high).toFixed(4);
    const low = sanitizeNumber(candle.low).toFixed(4);
    const volume = sanitizeNumber(candle.volume).toFixed(2);
    const trades = sanitizeNumber(candle.trades);
    row.innerHTML = `
      <td>${formatTs(candle.open_time)}</td>
      <td>${close}</td>
      <td>${high}</td>
      <td>${low}</td>
      <td>${volume}</td>
      <td>${trades}</td>
    `;
    body.appendChild(row);
  });
}

async function refreshJobs() {
  try {
    const data = await safeFetch("/api/backtest/jobs");
    updateJobsTable(data.jobs || []);
  } catch (err) {
    console.error(err);
  }
}

function init() {
  fillTimeframes(document.getElementById("timeframe"));
  fillTimeframes(document.getElementById("dataTimeframe"));

  document
    .getElementById("fetchForm")
    .addEventListener("submit", async (e) => {
      e.preventDefault();
      const msg = document.getElementById("fetchMessage");
      setMessage(msg, "提交中…");
      try {
        const payload = {
          exchange: document.getElementById("exchange").value.trim(),
          symbol: document.getElementById("symbol").value.trim(),
          timeframe: document.getElementById("timeframe").value,
          start_ts: tsFromInput(document.getElementById("start").value),
          end_ts: tsFromInput(document.getElementById("end").value),
        };
        if (!payload.symbol || !payload.start_ts || !payload.end_ts) {
          setMessage(msg, "请填写完整参数", "error");
          return;
        }
        const res = await safeFetch("/api/backtest/fetch", {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify(payload),
        });
        setMessage(
          msg,
          `任务 ${res.job.id} 已创建，状态：${res.job.status}`,
          "success"
        );
        refreshJobs();
      } catch (err) {
        setMessage(msg, err.message, "error");
      }
    });

  document
    .getElementById("refreshJobs")
    .addEventListener("click", refreshJobs);

  document
    .getElementById("candlesForm")
    .addEventListener("submit", async (e) => {
      e.preventDefault();
      const symbol = document.getElementById("dataSymbol").value.trim();
      const timeframe = document.getElementById("dataTimeframe").value.trim();
      if (!symbol || !timeframe) {
        alert("请填写交易对与周期");
        return;
      }
      const params = new URLSearchParams({ symbol, timeframe });
      try {
        const manifestRes = await safeFetch(
          `/api/backtest/data?${params.toString()}`
        );
        const manifest = manifestRes.manifest;
        const info = document.getElementById("manifestInfo");
        if (manifest) {
          info.textContent = `本地共有 ${manifest.rows} 根，时间范围 ${formatTs(
            manifest.min_time
          )} → ${formatTs(manifest.max_time)}`;
        } else {
          info.textContent = "未找到 manifest 信息";
        }
        const res = await safeFetch(
          `/api/backtest/candles/all?${params.toString()}`
        );
        updateCandlesTable(res.candles || []);
      } catch (err) {
        alert(err.message);
      }
    });

  // 任务表单默认时间设置（仅用于拉取任务）
  const now = new Date();
  const earlier = new Date(now.getTime() - 24 * 3600 * 1000);
  document.getElementById("end").value = toLocalInput(now);
  document.getElementById("start").value = toLocalInput(earlier);

  refreshJobs();
}

document.addEventListener("DOMContentLoaded", init);
