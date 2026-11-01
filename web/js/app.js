import { $ } from './util.js';
import { getConfig } from './maps.js';
import { enterGame, leaveGame } from './game.js';
import { enterRecords } from './records.js';
import { enterAdmin, leaveAdmin } from './admin.js';

const controllers = {
  home: {},
  game: { enter: enterGame, leave: leaveGame },
  records: { enter: enterRecords },
  admin: { enter: enterAdmin, leave: leaveAdmin },
};

let current = null;

function route() {
  const key = location.hash.replace(/^#\/?/, '') || 'home';
  const name = controllers[key] ? key : 'home';
  if (current && current !== name) controllers[current].leave?.();
  for (const n of Object.keys(controllers)) $(`view-${n}`).classList.toggle('hidden', n !== name);
  document.body.dataset.view = name;
  const prev = current;
  current = name;
  // 같은 화면 재진입(hashchange 없이)은 무시, 게임은 매번 새로 시작
  if (prev !== name) controllers[name].enter?.();
}

window.addEventListener('hashchange', route);
window.addEventListener('beforeunload', () => controllers[current]?.leave?.());

getConfig()
  .then((cfg) => { if (!cfg.googleMapsApiKey) $('api-warning').classList.remove('hidden'); })
  .catch(() => {});

route();
