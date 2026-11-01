import { $, api, formatDuration, formatDistance, formatDate, escapeHtml, toast, scoreColor } from './util.js';

let records = [];
let sortBy = 'recent';
let bound = false;

export async function enterRecords() {
  if (!bound) {
    bound = true;
    document.querySelectorAll('#view-records .seg-btn').forEach((b) =>
      b.addEventListener('click', () => {
        document.querySelectorAll('#view-records .seg-btn').forEach((x) => x.classList.toggle('active', x === b));
        sortBy = b.dataset.sort;
        renderTable();
      }));
  }
  try {
    const data = await api('GET', '/api/records?limit=200');
    records = data.records;
    renderStats(data.stats);
    renderTable();
  } catch (e) {
    toast(e.message, 'error');
  }
}

function renderStats(s) {
  const card = (label, value) => `<div class="stat"><label>${label}</label><b>${value}</b></div>`;
  $('records-stats').innerHTML = [
    card('총 게임 수', s.games.toLocaleString('ko-KR')),
    card('평균 스코어', s.games ? s.avgScore.toFixed(1) : '-'),
    card('최고 스코어', s.games ? s.bestScore.toFixed(1) : '-'),
    card('평균 오차 거리', s.games ? formatDistance(s.avgDistanceM) : '-'),
    card('평균 소요 시간', s.games ? formatDuration(s.avgDurationS) : '-'),
  ].join('');
}

function renderTable() {
  const rows = [...records];
  if (sortBy === 'score') rows.sort((a, b) => b.score - a.score || a.durationS - b.durationS);
  $('records-empty').classList.toggle('hidden', rows.length > 0);
  $('records-body').innerHTML = rows.map((r, i) => {
    const fb = r.feedback === 1 ? '👍' : r.feedback === -1 ? '👎' : '-';
    const addr = r.address ?? '(삭제된 위치)';
    return `<tr>
      <td>${i + 1}</td>
      <td>${formatDate(r.finishedAt)}</td>
      <td class="addr">${escapeHtml(addr)}</td>
      <td>${formatDistance(r.distanceM)}</td>
      <td>${formatDuration(r.durationS)}</td>
      <td><b style="color:${scoreColor(r.score)}">${r.score.toFixed(1)}</b></td>
      <td>${fb}</td>
    </tr>`;
  }).join('');
}
