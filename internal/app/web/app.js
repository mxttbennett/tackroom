const VIEWS = ['overview', 'skills', 'roles', 'wiring', 'unmanaged', 'crons', 'config'];
const baseURL = new URL(window.location.pathname.endsWith('/') ? window.location.pathname : `${window.location.pathname}/`, window.location.origin);

const CELLS = {
  ok: ['●', 'in place'],
  drift: ['~', 'drifted'],
  missing: ['+', 'missing'],
  conflict: ['!', 'conflict'],
  stale: ['×', 'stale'],
  off: ['·', 'not targeted'],
  disabled: ['–', 'turned off'],
  unsupported: ['', 'not supported'],
  absent: ['', 'not installed'],
  error: ['!', 'config unreadable'],
  unknown: ['?', 'unknown'],
};
const ATTENTION = new Set(['drift', 'missing', 'conflict', 'error', 'unknown']);

const store = {
  view: 'overview',
  inv: null,
  invError: '',
  shared: null,
  local: null,
  busy: false,
  skillQuery: '',
  skillFilter: 'all',
  openRows: new Set(),
  configLayer: 'shared',
  configState: null,
  configLoading: false,
  drafts: {},
  configDiff: '',
  configNote: '',
  preview: null,
  crons: null,
  cronsError: '',
  cronsLoading: false,
  cronFilter: 'all',
};

// ---------- helpers ----------
function h(tag, props = {}, ...children) {
  const node = document.createElement(tag);
  for (const [key, value] of Object.entries(props || {})) {
    if (value === undefined || value === null || value === false) continue;
    if (key === 'class') node.className = value;
    else if (key === 'text') node.textContent = value;
    else if (key.startsWith('on')) node.addEventListener(key.slice(2), value);
    else if (key === 'dataset') Object.assign(node.dataset, value);
    // CSSOM, not a style attribute: the view's CSP blocks inline style attributes.
    else if (key === 'style') Object.assign(node.style, value);
    else if (value === true) node.setAttribute(key, '');
    else node.setAttribute(key, value);
  }
  for (const child of children.flat()) {
    if (child === undefined || child === null || child === false) continue;
    node.append(child instanceof Node ? child : document.createTextNode(String(child)));
  }
  return node;
}
function mount(root, ...children) {
  root.replaceChildren(...children.flat().filter((child) => child !== null && child !== undefined && child !== false));
}
function tilde(path) {
  const home = store.inv?.home;
  if (!path || !home) return path || '';
  return path === home ? '~' : path.split(`${home}/`).join('~/');
}
function icon(id) {
  const svg = document.createElementNS('http://www.w3.org/2000/svg', 'svg');
  svg.setAttribute('aria-hidden', 'true');
  const use = document.createElementNS('http://www.w3.org/2000/svg', 'use');
  use.setAttribute('href', `#${id}`);
  svg.append(use);
  return svg;
}
function pick(object, ...keys) {
  for (const key of keys) if (object && object[key] !== undefined && object[key] !== null) return object[key];
  return undefined;
}
function csrf() {
  return document.cookie.split('; ').find((item) => item.startsWith('tackroom_csrf='))?.split('=')[1] || '';
}
async function api(path, options = {}) {
  const mutation = options.method && options.method !== 'GET';
  const headers = {Accept: 'application/json', ...(options.body ? {'Content-Type': 'application/json'} : {}), ...(mutation ? {'X-Tackroom-CSRF': csrf()} : {})};
  const response = await fetch(new URL(path.replace(/^\//, ''), baseURL), {...options, headers});
  const body = await response.json().catch(() => ({}));
  if (!response.ok) {
    const error = new Error(body.error?.message || `request failed (${response.status})`);
    error.code = body.error?.code || '';
    throw error;
  }
  return body;
}
let toastTimer;
function toast(message, bad = false) {
  const node = document.getElementById('toast');
  node.textContent = message;
  node.className = bad ? 'toast bad' : 'toast';
  node.hidden = false;
  clearTimeout(toastTimer);
  toastTimer = setTimeout(() => { node.hidden = true; }, bad ? 6000 : 3200);
}
function plural(count, word, many = `${word}s`) { return `${count} ${count === 1 ? word : many}`; }
function tokens(count) { return count >= 1000 ? `~${(count / 1000).toFixed(1)}k` : `~${count}`; }
function columns() { return (store.inv?.agents || []).filter((agent) => agent.inspected).map((agent) => agent.name); }
function agentOrder(names) {
  const order = (store.inv?.agents || []).map((agent) => agent.name);
  return [...names].sort((a, b) => {
    const ia = order.indexOf(a); const ib = order.indexOf(b);
    return (ia < 0 ? 999 : ia) - (ib < 0 ? 999 : ib) || a.localeCompare(b);
  });
}
function switchControl(checked, label, onchange, disabled = false) {
  const input = h('input', {type: 'checkbox', 'aria-label': label, disabled: disabled || store.busy || undefined, onchange});
  input.checked = !!checked;
  return h('label', {class: 'switch'}, input, h('span', {'aria-hidden': 'true'}));
}
function copyButton(text) {
  return h('button', {class: 'icon-button quiet', type: 'button', 'aria-label': `Copy ${text}`, onclick: async (event) => {
    try { await navigator.clipboard.writeText(text); toast('Copied to the clipboard.'); }
    catch { const code = event.currentTarget.previousElementSibling; if (code) window.getSelection().selectAllChildren(code); toast('Select the command and copy it.'); }
  }}, icon('i-copy'));
}

// ---------- config edits ----------
const SECTION_KEYS = {agents: ['Agents', 'agents'], mcp_servers: ['MCPServers', 'mcp_servers'], hooks: ['Hooks', 'hooks']};
function definedLocally(section, name) {
  const entries = pick(store.local?.typed_config, ...SECTION_KEYS[section]) || [];
  return entries.some((entry) => pick(entry, 'Name', 'name') === name);
}
async function patch(section, name, field, value, message) {
  const layer = definedLocally(section, name) ? 'local' : 'shared';
  const state = layer === 'local' ? store.local : store.shared;
  store.busy = true;
  try {
    await api('/api/config', {method: 'PATCH', body: JSON.stringify({layer, expected_revision: state.revision, operations: [{op: 'set', path: `/${section}/${name}/${field}`, value}]})});
    toast(`${message} Saved to ${layer === 'local' ? 'tackroom.local.yaml' : 'tackroom.yaml'}; Sync to apply it.`);
  } catch (error) {
    toast(error.code === 'stale_revision' ? 'The config changed on disk. Reloaded the latest version; try again.' : error.message, true);
  } finally {
    store.busy = false;
    await refresh();
  }
}

// ---------- data ----------
async function refresh() {
  const [inv, shared, local] = await Promise.allSettled([
    api('/api/inventory'),
    api('/api/state?layer=shared'),
    api('/api/state?layer=local'),
  ]);
  store.inv = inv.status === 'fulfilled' ? inv.value : null;
  store.invError = inv.status === 'rejected' ? inv.reason.message : '';
  store.shared = shared.status === 'fulfilled' ? shared.value : null;
  store.local = local.status === 'fulfilled' ? local.value : null;
  // Drop the cached Config layer so it reloads with the new revision; an
  // unsaved draft keeps its base, so saving it reports the conflict instead.
  if (!store.drafts[store.configLayer]) store.configState = null;
  renderChrome();
  renderView();
}

// ---------- chrome ----------
function renderChrome() {
  const inv = store.inv;
  const counts = {skills: '', roles: '', wiring: '', unmanaged: '', crons: ''};
  const alerts = {};
  if (inv) {
    const attention = inv.skills.filter((skill) => Object.values(skill.states).some((state) => ATTENTION.has(state))).length;
    counts.skills = attention || inv.skills.length;
    alerts.skills = attention > 0;
    counts.roles = inv.roles.length || '';
    counts.wiring = inv.mcp.length + inv.hooks.length || '';
    counts.unmanaged = inv.unmanaged.length || '';
  }
  if (store.crons) {
    const attention = store.crons.jobs.filter(cronNeedsAttention).length;
    counts.crons = attention || store.crons.jobs.length || '';
    alerts.crons = attention > 0;
  }
  document.querySelectorAll('[data-count]').forEach((node) => {
    node.textContent = counts[node.dataset.count] ?? '';
    node.classList.toggle('alert', !!alerts[node.dataset.count]);
  });
  const pending = (inv?.agents || []).reduce((sum, agent) => sum + (agent.inspected ? agent.pending : 0), 0);
  const unsynced = (inv?.agents || []).filter((agent) => agent.inspected && agent.detected && !agent.error && !agent.synced).length;
  const button = document.getElementById('open-sync');
  button.classList.toggle('pending', pending > 0 || unsynced > 0);
  document.getElementById('sync-label').textContent = pending > 0 ? `Review sync · ${pending}` : unsynced > 0 ? 'Review sync' : 'Sync';
  const links = pick(store.shared?.effective_ui, 'Links', 'links') || [];
  mount(document.getElementById('links'), ...links.map((link) => h('a', {href: pick(link, 'URL', 'url'), target: '_top', text: pick(link, 'Name', 'name')})));
  document.getElementById('links-menu').hidden = links.length === 0;
  document.getElementById('scope').textContent = store.shared?.paths?.shared ? tilde(store.shared.paths.shared) : '';
}

function route() {
  const next = window.location.hash.replace('#', '');
  store.view = VIEWS.includes(next) ? next : 'overview';
  document.querySelectorAll('.view').forEach((node) => { node.hidden = node.dataset.view !== store.view; });
  document.querySelectorAll('.tabs a').forEach((node) => {
    if (node.dataset.view === store.view) node.setAttribute('aria-current', 'page');
    else node.removeAttribute('aria-current');
  });
  if (store.view === 'crons' && !store.crons && !store.cronsLoading) loadCrons();
  document.getElementById('crumb').textContent = document.querySelector(`.tabs a[data-view="${store.view}"] .label`)?.textContent || '';
  document.body.classList.remove('nav-open');
  document.getElementById('open-nav').setAttribute('aria-expanded', 'false');
  renderView();
}

function renderView() {
  const root = document.getElementById(`view-${store.view}`);
  if (store.view === 'config') { renderConfig(root); return; }
  if (store.view === 'crons') { renderCrons(root); return; }
  if (!store.inv) {
    mount(root, store.invError
      ? h('div', {class: 'error-box'}, h('strong', {text: 'Could not inspect your agents. '}), store.invError, ' Fix the config in the ', h('a', {href: '#config', text: 'Config'}), ' view.')
      : h('p', {class: 'loading', text: 'Inspecting your agents…'}));
    return;
  }
  ({overview: renderOverview, skills: renderSkills, roles: renderRoles, wiring: renderWiring, unmanaged: renderUnmanaged})[store.view](root);
}

function viewHead(title, lede, ...extra) {
  return h('div', {class: 'view-head'}, h('div', {}, h('h1', {text: title}), lede ? h('p', {class: 'lede', text: lede}) : null), ...extra);
}

// ---------- overview ----------
function agentStatus(agent) {
  if (!agent.enabled) return ['', 'off'];
  if (!agent.inspected) return ['', 'not inspected'];
  if (agent.error) return ['bad', 'config unreadable'];
  if (!agent.detected) return ['', 'not installed'];
  if (agent.pending > 0) return ['warn', `${agent.pending} pending`];
  if (agent.synced) return ['ok', 'synced'];
  return ['warn', 'needs sync'];
}

function renderOverview(root) {
  const inv = store.inv;
  const enabled = inv.agents.filter((agent) => agent.enabled);
  const pending = inv.agents.reduce((sum, agent) => sum + (agent.inspected ? agent.pending : 0), 0);
  const unsynced = inv.agents.filter((agent) => agent.inspected && agent.detected && !agent.error && !agent.synced).length;
  const state = pending > 0
    ? h('span', {class: 'pill warn', text: `${plural(pending, 'change')} waiting`})
    : unsynced > 0 ? h('span', {class: 'pill warn', text: `${plural(unsynced, 'agent')} out of sync`}) : h('span', {class: 'pill ok', text: 'All agents synced'});
  const stats = h('div', {class: 'stats'},
    stat(`${enabled.length}/${inv.agents.length}`, 'Agents on'),
    stat(inv.skills.length, 'Skills'),
    stat(inv.roles.length, 'Roles'),
    stat(inv.mcp.length, 'MCP servers'),
    stat(inv.hooks.length, 'Hooks'),
    h('div', {class: 'stat-state'}, state, h('span', {class: 'mono faint', text: `revision ${(inv.revision || '').slice(0, 10)}`})));

  const issues = [];
  for (const agent of inv.agents.filter((item) => item.inspected)) {
    if (agent.error) issues.push([`${agent.name}: config unreadable (${agent.error})`, '#config', 'Open config']);
    if (agent.counts.drifted) issues.push([`${agent.name}: ${plural(agent.counts.drifted, 'drifted skill link')}`, '#skills', 'See skills']);
    if (agent.counts.missing) issues.push([`${agent.name}: ${plural(agent.counts.missing, 'skill')} not linked yet`, '#skills', 'See skills']);
    if (agent.counts.conflicts) issues.push([`${agent.name}: ${plural(agent.counts.conflicts, 'conflict')} with files tackroom did not write`, '#unmanaged', 'See conflicts']);
    if (agent.root_state && agent.root_state !== 'synced') issues.push([`${agent.name}: root instructions ${agent.root_state}`, null, null]);
  }
  if (inv.repo_state && inv.repo_state !== 'synced') issues.unshift([`~/.agents link is ${inv.repo_state}`, null, null]);
  const cronTrouble = (store.crons?.jobs || []).filter(cronNeedsAttention).length;
  if (cronTrouble) issues.push([`Crons: ${cronFinding(store.crons.jobs, store.crons.jobs.filter(cronNeedsAttention))}`, '#crons', 'See crons']);
  const attention = h('div', {class: 'attention'},
    h('h2', {text: 'Needs attention'}),
    issues.length
      ? h('ul', {}, issues.map(([text, href, label]) => h('li', {}, h('span', {text}), href ? h('a', {href, text: label}) : null)))
      : h('p', {class: 'muted', text: inv.unmanaged.length ? `Nothing blocks sync. ${plural(inv.unmanaged.length, 'item')} in agent folders ${inv.unmanaged.length === 1 ? 'is' : 'are'} not managed by tackroom.` : 'Nothing needs attention.'}));

  const cards = h('div', {class: 'cards'}, inv.agents.map(agentCard));
  mount(root, 
    viewHead('Agents', 'What tackroom manages on each coding agent. Turn an agent off to stop syncing it; nothing on disk is removed.'),
    stats, attention, h('h2', {text: 'Agents'}), cards);
}

function stat(value, label) { return h('div', {class: 'stat'}, h('b', {text: String(value)}), h('span', {text: label})); }

function agentCard(agent) {
  const [kind, label] = agentStatus(agent);
  const local = definedLocally('agents', agent.name);
  const toggle = switchControl(agent.enabled, `${agent.name} enabled`, (event) => patch('agents', agent.name, 'enabled', event.target.checked, `${agent.name} ${event.target.checked ? 'turned on' : 'turned off'}.`));
  const head = h('div', {class: 'card-head'}, h('div', {}, h('h3', {text: agent.name}), local ? h('span', {class: 'tag local', text: 'local'}) : null), toggle);
  const children = [head, h('div', {}, h('span', {class: `pill ${kind}`, text: label}))];
  if (agent.inspected && agent.detected && !agent.error) {
    const c = agent.counts;
    children.push(h('div', {class: 'tally'},
      tally(c.managed, 'linked'),
      tally(c.drifted, 'drifted', c.drifted ? 'warn' : ''),
      tally(c.missing, 'missing', c.missing ? 'warn' : ''),
      tally(c.unmanaged, 'unmanaged')));
    const supports = [agent.supports_mcp ? 'MCP' : null, agent.supports_hooks ? 'hooks' : null, agent.supports_roles ? 'roles' : null].filter(Boolean);
    children.push(h('div', {class: 'card-foot'},
      h('span', {text: `${tokens(agent.tokens)} tokens of skill listings (est.)`}),
      h('span', {text: supports.length ? supports.join(' · ') : 'skills only'})));
  } else if (agent.error) {
    children.push(h('p', {class: 'item-detail', text: agent.error}));
  } else if (!agent.enabled || !agent.detected) {
    // Same rows as an inspected agent, with dashes, so the cards in a row end level.
    children.push(h('div', {class: 'tally'}, tally('-', 'linked'), tally('-', 'drifted'), tally('-', 'missing'), tally('-', 'unmanaged')));
    children.push(h('div', {class: 'card-foot'}, h('span', {text: agent.enabled ? 'Not found on this machine' : 'Turned off: sync leaves it alone'})));
  }
  if (agent.skill_root) children.push(h('p', {class: 'path', text: tilde(agent.skill_root)}));
  return h('article', {class: agent.enabled ? 'card' : 'card off'}, children);
}
function tally(value, label, kind = '') { return h('div', {class: kind}, h('b', {text: String(value || 0)}), h('span', {text: label})); }

// ---------- matrices ----------
function cell(state, agent) {
  const [glyph, word] = CELLS[state] || CELLS.unknown;
  return h('span', {class: `cell ${state}`, title: `${agent}: ${word}`, 'aria-label': `${agent}: ${word}`, text: glyph});
}
function legend(states) {
  return h('div', {class: 'legend'}, states.map((state) => h('span', {}, h('span', {class: `cell ${state}`, 'aria-hidden': 'true', text: CELLS[state][0]}), CELLS[state][1])));
}
function matrixHead(lead) {
  return h('thead', {}, h('tr', {}, h('th', {class: 'lead', scope: 'col', text: lead}), columns().map((name) => h('th', {scope: 'col', text: name}))));
}

function renderSkills(root) {
  const inv = store.inv;
  const query = store.skillQuery.trim().toLowerCase();
  const rows = inv.skills.filter((skill) => {
    if (query && !`${skill.name} ${skill.description} ${skill.origin}`.toLowerCase().includes(query)) return false;
    if (store.skillFilter === 'attention') return Object.values(skill.states).some((state) => ATTENTION.has(state));
    if (store.skillFilter === 'local') return skill.origin === 'local';
    if (store.skillFilter === 'external') return skill.origin !== 'local';
    return true;
  });
  const search = h('input', {class: 'search', type: 'search', placeholder: 'Search skills', 'aria-label': 'Search skills', value: store.skillQuery, oninput: (event) => {
    store.skillQuery = event.target.value;
    const caret = event.target.selectionStart;
    renderView();
    const next = document.querySelector('#view-skills .search');
    next.focus(); next.setSelectionRange(caret, caret);
  }});
  const filters = segmented([['all', 'All'], ['attention', 'Needs attention'], ['local', 'Local'], ['external', 'External']], store.skillFilter, (value) => { store.skillFilter = value; renderView(); });
  const tbody = h('tbody', {});
  for (const skill of rows) {
    const open = store.openRows.has(`skill:${skill.name}`);
    const row = h('tr', {class: 'row', tabindex: '0', 'aria-expanded': String(open), onclick: () => toggleRow(`skill:${skill.name}`), onkeydown: (event) => { if (event.key === 'Enter' || event.key === ' ') { event.preventDefault(); toggleRow(`skill:${skill.name}`); } }},
      h('td', {class: 'lead'},
        h('div', {class: 'lead-title'}, skill.name, h('span', {class: 'tag', text: skill.origin})),
        skill.description ? h('div', {class: 'lead-desc', text: skill.description}) : null),
      columns().map((agent) => h('td', {}, cell(skill.states[agent] || 'unknown', agent))));
    tbody.append(row);
    if (open) {
      tbody.append(h('tr', {class: 'detail'}, h('td', {colspan: String(columns().length + 1)}, h('div', {class: 'detail-grid'},
        skill.description ? h('p', {text: skill.description}) : null,
        h('dl', {},
          h('dt', {text: 'origin'}), h('dd', {text: skill.origin}),
          h('dt', {text: 'path'}), h('dd', {class: 'mono', text: tilde(skill.path)}),
          h('dt', {text: 'listing'}), h('dd', {text: `${tokens(skill.tokens)} tokens (est.)`}),
          columns().flatMap((agent) => [h('dt', {text: agent}), h('dd', {text: (CELLS[skill.states[agent]] || CELLS.unknown)[1]})]))))));
    }
  }
  const table = rows.length
    ? h('div', {class: 'matrix-wrap'}, h('table', {class: 'matrix'}, matrixHead(`${plural(rows.length, 'skill')}`), tbody))
    : h('p', {class: 'empty', text: inv.skills.length ? 'No skills match this filter.' : 'No canonical skills yet. Add one with tackroom skill new <name>.'});
  mount(root, 
    viewHead('Skills', 'Every canonical skill goes to every enabled agent. Each cell shows what is on disk now; Sync fixes drifted and missing links. Select a row for details.'),
    h('div', {class: 'toolbar'}, search, filters),
    legend(['ok', 'drift', 'missing', 'conflict', 'absent']),
    table);
}
function toggleRow(key) {
  if (store.openRows.has(key)) store.openRows.delete(key); else store.openRows.add(key);
  renderView();
}
function segmented(options, current, onpick) {
  return h('div', {class: 'segmented', role: 'group'}, options.map(([value, label]) => h('button', {type: 'button', 'aria-pressed': String(value === current), onclick: () => onpick(value), text: label})));
}

function renderRoles(root) {
  const inv = store.inv;
  const body = inv.roles.length
    ? h('div', {class: 'matrix-wrap'}, h('table', {class: 'matrix'}, matrixHead(plural(inv.roles.length, 'role')),
      h('tbody', {}, inv.roles.map((role) => h('tr', {}, h('td', {class: 'lead'}, h('div', {class: 'lead-title', text: role.name})), columns().map((agent) => h('td', {}, cell(role.states[agent] || 'unknown', agent))))))))
    : h('p', {class: 'empty', text: 'No agent roles are configured. Roles live in agents/*.md in your config root.'});
  mount(root, 
    viewHead('Roles', 'Agent roles from agents/*.md, rendered into each agent that supports subagents.'),
    legend(['ok', 'drift', 'missing', 'unsupported']),
    body);
}

// ---------- wiring ----------
function renderWiring(root) {
  const inv = store.inv;
  mount(root, 
    viewHead('MCP & hooks', 'Choose which agents get each MCP server and hook. Select a cell to add or remove that agent. Changes save to your config right away; Sync writes them to the agents.'),
    legend(['ok', 'missing', 'drift', 'off', 'disabled', 'unsupported']),
    h('h2', {text: 'MCP servers'}),
    wiringTable('mcp', inv.mcp),
    h('h2', {text: 'Hooks'}),
    wiringTable('hooks', inv.hooks));
}
function wiringTable(kind, rows) {
  if (!rows.length) return h('p', {class: 'empty', text: kind === 'mcp' ? 'No MCP servers yet. Add one with tackroom mcp add.' : 'No hooks yet. Add them under hooks: in tackroom.yaml.'});
  const section = kind === 'mcp' ? 'mcp_servers' : 'hooks';
  const supportKey = kind === 'mcp' ? 'supports_mcp' : 'supports_hooks';
  return h('div', {class: 'matrix-wrap'}, h('table', {class: 'matrix'}, matrixHead(kind === 'mcp' ? 'Server' : 'Hook'),
    h('tbody', {}, rows.map((row) => {
      const local = definedLocally(section, row.name);
      const lead = h('td', {class: 'lead'}, h('div', {class: 'lead-row'},
        switchControl(row.enabled, `${row.name} enabled`, (event) => patch(section, row.name, 'enabled', event.target.checked, `${row.name} ${event.target.checked ? 'turned on' : 'turned off'}.`)),
        h('div', {},
          h('div', {class: 'lead-title'}, row.name,
            row.event ? h('span', {class: 'tag', text: row.event}) : null,
            row.explicit ? null : h('span', {class: 'tag', text: 'all agents'}),
            local ? h('span', {class: 'tag local', text: 'local'}) : null),
          h('div', {class: 'lead-cmd', text: row.command}))));
      const cells = columns().map((agent) => {
        const state = row.states[agent] || 'unknown';
        const targeted = !['off', 'unsupported', 'absent', 'error'].includes(state);
        const locked = ['unsupported', 'absent', 'error'].includes(state) || store.busy;
        const [glyph, word] = CELLS[state] || CELLS.unknown;
        return h('td', {}, h('button', {
          type: 'button', class: `cell ${state}`, text: glyph, disabled: locked || undefined,
          'aria-pressed': String(targeted), 'aria-label': `${row.name} on ${agent}: ${word}`,
          title: locked ? `${agent}: ${word}` : `${agent}: ${word}. Select to ${targeted ? 'remove' : 'add'} this agent.`,
          onclick: () => toggleTarget(section, supportKey, row, agent, targeted),
        }));
      });
      return h('tr', {}, lead, cells);
    }))));
}
async function toggleTarget(section, supportKey, row, agent, targeted) {
  const universe = store.inv.agents.filter((item) => item[supportKey]).map((item) => item.name);
  const current = row.explicit ? [...row.targets] : universe;
  const next = targeted ? current.filter((name) => name !== agent) : [...new Set([...current, agent])];
  if (!next.length) {
    toast('At least one agent must stay targeted. Turn the entry off instead.', true);
    return;
  }
  await patch(section, row.name, 'agents', agentOrder(next), `${row.name} ${targeted ? 'removed from' : 'added to'} ${agent}.`);
}

// ---------- unmanaged ----------
const UNMANAGED_GROUPS = [
  ['skill', 'Skills in agent folders', 'tackroom found these next to its own links and leaves them alone. Promote one to manage it from your config root.'],
  ['conflict', 'Conflicts', 'A native file differs from what tackroom would write, so sync skips it.'],
  ['stale', 'Stale links', 'Links into the tackroom store for skills that are no longer canonical. Sync removes them.'],
];
function renderUnmanaged(root) {
  const items = store.inv.unmanaged;
  const groups = UNMANAGED_GROUPS.map(([kind, title, lede]) => {
    const list = items.filter((item) => item.kind === kind);
    if (!list.length) return null;
    return h('section', {class: 'group'},
      h('div', {}, h('h2', {text: `${title} · ${list.length}`}), h('p', {class: 'muted', text: lede})),
      h('div', {class: 'items'}, list.map((item) => h('div', {class: 'item'},
        h('div', {class: 'item-main'},
          h('span', {class: 'item-name', text: item.name}),
          item.detail ? h('span', {class: 'item-detail', text: tilde(item.detail)}) : null,
          h('div', {class: 'chips'}, item.agents.map((agent) => h('span', {class: 'tag', text: agent})))),
        item.hint ? h('div', {class: 'hint'}, h('code', {text: tilde(item.hint)}), copyButton(tilde(item.hint))) : null))));
  }).filter(Boolean);
  mount(root, 
    viewHead('Unmanaged', 'Things in your agents\' folders that tackroom did not put there. tackroom never deletes them.'),
    ...(groups.length ? groups : [h('p', {class: 'empty', text: 'Everything in your agents\' skill folders is managed by tackroom.'})]));
}

// ---------- crons ----------
const CRON_OK_STATUS = new Set(['ok', 'success', 'succeeded', 'completed']);
const DAY_NAMES = ['Sun', 'Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat'];
const STRIP_HOURS = 24;

async function loadCrons(refresh = false) {
  store.cronsLoading = true;
  if (store.view === 'crons') renderView();
  try {
    store.crons = await api(`/api/crons${refresh ? '?refresh=1' : ''}`);
    store.cronsError = '';
  } catch (error) {
    store.cronsError = error.message;
  }
  store.cronsLoading = false;
  renderChrome();
  if (store.view === 'crons' || store.view === 'overview') renderView();
}
// A paused or disabled job's old error is not actionable until it is turned on again.
function cronNeedsAttention(job) {
  if (['disabled', 'paused', 'not_loaded'].includes(job.state)) return false;
  return (job.problems || []).length > 0 || ['dead', 'missing'].includes(job.state);
}
function ago(iso) {
  const time = Date.parse(iso);
  if (!time) return '';
  const minutes = Math.round((Date.now() - time) / 60000);
  if (minutes < 2) return 'just now';
  if (minutes < 90) return `${minutes} min ago`;
  const hours = Math.round(minutes / 60);
  if (hours < 36) return `${hours} h ago`;
  return `${Math.round(hours / 24)} d ago`;
}
function until(ms) {
  const minutes = Math.max(1, Math.round(ms / 60000));
  if (minutes < 60) return `in ${minutes} min`;
  const hours = Math.floor(minutes / 60);
  return minutes % 60 && hours < 10 ? `in ${hours} h ${minutes % 60} min` : `in ${Math.round(minutes / 60)} h`;
}
function clock(ms) { return new Date(ms).toLocaleTimeString([], {hour: '2-digit', minute: '2-digit', hourCycle: 'h23'}); }
function grouped(text) { return String(text).replace(/\d{4,}/g, (digits) => Number(digits).toLocaleString('en-US')); }
function sentence(text) { return text ? text[0].toUpperCase() + text.slice(1) : ''; }

// Cron fields: minute hour day-of-month month day-of-week, with *, lists, ranges and steps.
function cronField(text, min, max) {
  const values = new Set();
  for (const part of text.split(',')) {
    const [range, stepText] = part.split('/');
    const step = Number(stepText || 1);
    let [lo, hi] = range === '*' ? [min, max] : range.split('-').map(Number);
    if (hi === undefined) hi = stepText ? max : lo;
    if (![lo, hi, step].every(Number.isFinite) || step < 1 || lo < min || hi > max) return null;
    for (let value = lo; value <= hi; value += step) values.add(max === 7 && value === 7 ? 0 : value);
  }
  return values;
}
function parseCron(expr) {
  const fields = expr.trim().split(/\s+/);
  if (fields.length !== 5) return null;
  const spec = {m: cronField(fields[0], 0, 59), h: cronField(fields[1], 0, 23), dom: cronField(fields[2], 1, 31), mon: cronField(fields[3], 1, 12), dow: cronField(fields[4], 0, 7)};
  if (Object.values(spec).some((value) => value === null)) return null;
  return {...spec, domAny: fields[2] === '*', dowAny: fields[4] === '*'};
}
// `wall` carries the job's local time in its UTC fields.
function cronMatches(spec, wall) {
  if (!spec.m.has(wall.getUTCMinutes()) || !spec.h.has(wall.getUTCHours()) || !spec.mon.has(wall.getUTCMonth() + 1)) return false;
  const dom = spec.dom.has(wall.getUTCDate());
  const dow = spec.dow.has(wall.getUTCDay());
  return spec.domAny || spec.dowAny ? dom && dow : dom || dow;
}
// Minutes east of UTC for an IANA zone or a +HH:MM offset.
function zoneOffset(tz, at) {
  const fixed = /^([+-])(\d\d):(\d\d)$/.exec(tz || '');
  if (fixed) return (fixed[1] === '-' ? -1 : 1) * (Number(fixed[2]) * 60 + Number(fixed[3]));
  try {
    const parts = Object.fromEntries(new Intl.DateTimeFormat('en-US', {timeZone: tz, hourCycle: 'h23', year: 'numeric', month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit'})
      .formatToParts(at).map((part) => [part.type, part.value]));
    return Math.round((Date.UTC(+parts.year, parts.month - 1, +parts.day, +parts.hour, +parts.minute) - Math.floor(at / 60000) * 60000) / 60000);
  } catch {
    return -at.getTimezoneOffset();
  }
}
// Fire times in the next STRIP_HOURS for cron and interval schedules; null for services and unknown forms.
function cronFires(job, from) {
  if (job.state === 'service') return null;
  const schedule = job.schedule || '';
  const end = from + STRIP_HOURS * 3600000;
  const every = /^every (\d+)s$/.exec(schedule);
  if (every) {
    const step = Number(every[1]) * 1000;
    const base = Date.parse(job.last_run) || from;
    let next = base + step * Math.ceil(Math.max(0, from - base) / step);
    const fires = [];
    for (; next < end && fires.length < 2000; next += step) fires.push(next);
    return {fires, interval: Number(every[1])};
  }
  const specs = schedule.split(';').map(parseCron);
  if (!specs.length || specs.some((spec) => !spec)) return null;
  const start = Math.ceil(from / 60000) * 60000;
  const offset = zoneOffset(job.tz, new Date(start)) * 60000;
  const fires = [];
  for (let t = start; t < end; t += 60000) {
    const wall = new Date(t + offset);
    if (specs.some((spec) => cronMatches(spec, wall))) fires.push(t);
  }
  return {fires};
}
// The first fire within a week, for jobs that do not run in the next STRIP_HOURS.
function nextFire(job, from) {
  const specs = (job.schedule || '').split(';').map(parseCron);
  if (!specs.length || specs.some((spec) => !spec)) return null;
  const start = Math.ceil(from / 60000) * 60000;
  const offset = zoneOffset(job.tz, new Date(start)) * 60000;
  for (let t = start; t < start + 8 * 86400000; t += 60000) {
    if (specs.some((spec) => cronMatches(spec, new Date(t + offset)))) return t;
  }
  return null;
}
function weekdayClock(ms) { return `${DAY_NAMES[new Date(ms).getDay()]} ${clock(ms)}`; }
function duration(seconds) {
  if (seconds % 3600 === 0) return seconds === 3600 ? 'hour' : `${seconds / 3600} h`;
  if (seconds % 60 === 0) return seconds === 60 ? 'minute' : `${seconds / 60} min`;
  return `${seconds} s`;
}
function describeSchedule(job) {
  const schedule = job.schedule || '';
  if (job.state === 'service') return 'Always on';
  const every = /^every (\d+)s$/.exec(schedule);
  if (every) return `Every ${duration(Number(every[1]))}`;
  const lines = schedule.split(';').map((line) => line.trim().split(/\s+/));
  if (!lines.length || lines.some((fields) => fields.length !== 5)) return schedule;
  const zone = zoneOffset(job.tz, new Date()) === -new Date().getTimezoneOffset() ? '' : ` (${job.tz})`;
  const [m, hr, dom, mon, dow] = lines[0];
  const step = /^\*\/(\d+)$/.exec(m);
  if (lines.length === 1 && step && hr === '*' && dom === '*' && mon === '*' && dow === '*') return `Every ${step[1]} min`;
  const sameTime = lines.every((fields) => fields[0] === m && fields[1] === hr && fields[2] === '*' && fields[3] === '*');
  if (!sameTime || !/^\d+$/.test(m) || !/^\d+$/.test(hr)) return schedule;
  const time = `${hr.padStart(2, '0')}:${m.padStart(2, '0')}`;
  const days = new Set();
  for (const fields of lines) {
    const set = cronField(fields[4], 0, 7);
    if (!set) return schedule;
    set.forEach((day) => days.add(day));
  }
  if (days.size === 7) return `Daily ${time}${zone}`;
  if (days.size === 5 && [1, 2, 3, 4, 5].every((day) => days.has(day))) return `Weekdays ${time}${zone}`;
  const order = [...days].sort((a, b) => ((a + 6) % 7) - ((b + 6) % 7));
  return `${order.map((day) => DAY_NAMES[day]).join(', ')} ${time}${zone}`;
}
function cronProblem(job) {
  const problem = (job.problems || [])[0] || job.state;
  return sentence(grouped(problem.replace(/^last error: /, 'Last error: ')));
}
function cronFinding(jobs, attention) {
  if (!attention.length) return `Nothing failing across ${plural(jobs.length, 'job')}.`;
  const has = (job, text) => (job.problems || []).some((problem) => problem.includes(text));
  const loops = attention.filter((job) => has(job, 'crash loop')).length;
  const missed = attention.filter((job) => has(job, 'has not run')).length;
  const other = attention.length - loops - missed;
  const kinds = [loops && plural(loops, 'crash loop'), missed && plural(missed, 'missed schedule'), other && plural(other, 'failing job')].filter(Boolean);
  return `${plural(attention.length, 'job')} need${attention.length === 1 ? 's' : ''} attention: ${kinds.join(', ')}.`;
}
function cronResult(job) {
  if (job.last_exit != null) return job.last_exit === 0 ? h('span', {class: 'faint', text: 'exit 0'}) : h('span', {class: 'pill bad', text: `exit ${job.last_exit}`});
  if (job.last_status) return CRON_OK_STATUS.has(job.last_status) ? h('span', {class: 'faint', text: job.last_status}) : h('span', {class: 'pill bad', text: job.last_status.replace('_', ' ')});
  return h('span', {class: 'faint', text: '-'});
}
function cronKey(job) { return `cron:${job.host}/${job.source}/${job.id}`; }
function hostName(host) { return host === 'local' ? 'This Mac' : host; }
function openCron(job) {
  store.openRows.add(cronKey(job));
  renderView();
  document.getElementById(cronKey(job))?.scrollIntoView({block: 'center'});
}

function renderCrons(root) {
  const data = store.crons;
  const stamp = data ? h('span', {class: 'stamp', text: `Collected ${clock(Date.parse(data.scanned_at))}`}) : null;
  const refresh = h('button', {type: 'button', disabled: store.cronsLoading || undefined, onclick: () => loadCrons(true), text: store.cronsLoading ? 'Collecting…' : 'Collect again'});
  const jobs = data?.jobs || [];
  const attention = jobs.filter(cronNeedsAttention);
  const head = viewHead('Crons', data ? cronFinding(jobs, attention) : 'Scheduled jobs and failing services on every host in crons.hosts.', h('div', {class: 'title-actions'}, stamp, refresh));
  if (!data) {
    if (store.cronsError) {
      mount(root, head, h('div', {class: 'error-box'}, h('strong', {text: 'Could not collect scheduled jobs. '}), store.cronsError, ' ', h('button', {type: 'button', class: 'quiet', onclick: () => loadCrons(true), text: 'Retry'})));
      return;
    }
    // The final layout with its figures still empty, so nothing moves when the data lands.
    const waiting = (title) => h('section', {class: 'panel-block'}, h('div', {class: 'block-head'}, h('h2', {text: title})), h('p', {class: 'block-empty', text: 'Collecting from every host in crons.hosts. SSH hosts take a few seconds.'}));
    mount(root, head, h('div', {class: 'kpis'}, kpi('Scheduled jobs', '-'), kpi('Failing services', '-'), kpi('Need attention', '-'), kpi('Hosts read', '-')), waiting('Needs attention'), waiting('Next 24 hours'));
    return;
  }
  const hosts = [...new Set([...(data.access || []).map((entry) => entry.host), ...jobs.map((job) => job.host)])];
  const reached = hosts.filter((host) => (data.access || []).some((entry) => entry.host === host && entry.state !== 'not_scanned'));
  const services = jobs.filter((job) => job.state === 'service').length;
  const kpis = h('div', {class: 'kpis'},
    kpi('Scheduled jobs', jobs.length - services),
    kpi('Failing services', services, services > 0),
    kpi('Need attention', attention.length, attention.length > 0),
    kpi('Hosts read', `${reached.length} of ${hosts.length}`, reached.length < hosts.length));
  const blocks = [head, kpis];
  if (attention.length) blocks.push(attentionPanel(attention));
  blocks.push(fireStrip(jobs, hosts));
  for (const host of hosts) blocks.push(hostSection(host, data));
  blocks.push(h('p', {class: 'footnote', text: 'Hosts come from crons.hosts in tackroom.local.yaml. The CLI shows the same: tackroom cron collect --table.'}));
  mount(root, ...blocks);
}
function kpi(label, value, bad = false) {
  return h('div', {class: 'kpi'}, h('span', {class: 'kpi-label', text: label}), h('b', {class: bad ? 'kpi-value bad' : 'kpi-value', text: String(value)}));
}
function attentionPanel(attention) {
  const showAll = store.openRows.has('cron:attention-all');
  const rows = showAll ? attention : attention.slice(0, 4);
  return h('section', {class: 'panel-block'},
    h('div', {class: 'block-head'}, h('h2', {text: 'Needs attention'}), h('span', {class: 'block-meta', text: plural(attention.length, 'job')})),
    h('div', {class: 'block-body'}, h('table', {class: 'grid-table attention-table'},
      h('thead', {}, h('tr', {}, ['Job', 'Host', 'Problem', 'Last run', ''].map((label) => h('th', {scope: 'col', text: label})))),
      h('tbody', {}, rows.map((job) => h('tr', {},
        h('td', {class: 'ident'}, h('span', {class: 'ident-name', text: job.name || job.id}), h('span', {class: 'ident-sub', text: job.source})),
        h('td', {class: 'muted nowrap', text: hostName(job.host)}),
        h('td', {class: 'problem', text: cronProblem(job)}),
        h('td', {class: 'muted nowrap fig', title: job.last_run || '', text: job.last_run ? ago(job.last_run) : '-'}),
        h('td', {class: 'action'}, h('button', {type: 'button', class: 'quiet', onclick: () => openCron(job), text: 'Details'}))))))),
    attention.length > 4 ? h('div', {class: 'block-foot'}, h('button', {type: 'button', class: 'quiet', onclick: () => toggleRow('cron:attention-all'), text: showAll ? 'Show fewer' : `Show all ${attention.length}`})) : null);
}
function fireStrip(jobs, hosts) {
  const now = Date.now();
  const rows = [];
  for (const host of hosts) {
    for (const job of jobs.filter((item) => item.host === host && !['disabled', 'paused', 'not_loaded'].includes(item.state))) {
      const fires = cronFires(job, now);
      if (fires) rows.push({job, ...fires});
    }
  }
  rows.sort((a, b) => (a.job.host === b.job.host ? (a.fires[0] ?? Infinity) - (b.fires[0] ?? Infinity) : hosts.indexOf(a.job.host) - hosts.indexOf(b.job.host)));
  const span = STRIP_HOURS * 3600000;
  const at = (t) => `${(((t - now) / span) * 100).toFixed(3)}%`;
  const firstHour = Math.ceil(now / 3600000) * 3600000;
  const ticks = [];
  for (let t = firstHour; t < now + span; t += 3600000) if (new Date(t).getHours() % 6 === 0) ticks.push(t);
  const axis = h('div', {class: 'strip-row strip-axis'}, h('span', {class: 'strip-label'}),
    h('div', {class: 'strip-track'}, h('span', {class: 'strip-now', style: {left: '0'}, text: `Now ${clock(now)}`}), ticks.filter((t) => t - now > 2 * 3600000).map((t) => h('span', {class: 'strip-hour', style: {left: at(t)}, text: clock(t)}))));
  const body = rows.map(({job, fires, interval}) => {
    const dense = interval ? interval < 1800 : fires.length > 48;
    const track = h('div', {class: 'strip-track', title: `${describeSchedule(job)}${fires[0] ? `, next ${clock(fires[0])}` : ''}`},
      ticks.map((t) => h('span', {class: 'strip-grid', style: {left: at(t)}})),
      dense
        ? h('span', {class: 'strip-band', style: {left: at(fires[0] ?? now), right: '0'}})
        : fires.map((t) => h('span', {class: 'strip-fire', style: {left: at(t)}})),
      fires.length ? null : h('span', {class: 'strip-later', text: (() => { const t = nextFire(job, now); return t ? `Next run ${weekdayClock(t)}` : 'No run within a week'; })()}));
    return h('div', {class: cronNeedsAttention(job) ? 'strip-row trouble' : 'strip-row'},
      h('span', {class: 'strip-label'}, h('span', {class: 'strip-name', text: job.name || job.id}), h('span', {class: 'strip-host', text: `${hostName(job.host)} · ${describeSchedule(job)}`})),
      track);
  });
  return h('section', {class: 'panel-block strip'},
    h('div', {class: 'block-head'}, h('h2', {text: 'Next 24 hours'}), h('span', {class: 'block-meta', text: `${plural(rows.length, 'scheduled job')} · each mark is one run`})),
    h('div', {class: 'block-body'}, axis, ...body));
}
function hostSection(host, data) {
  const access = (data.access || []).filter((entry) => entry.host === host);
  const unreachable = access.length > 0 && access.every((entry) => entry.state === 'not_scanned') && access.some((entry) => (entry.detail || '').startsWith('probe failed'));
  const rows = data.jobs.filter((job) => job.host === host);
  const sources = access.filter((entry) => entry.state !== 'not_scanned' || !/Linux only|cloud routines|not readable/.test(entry.detail || ''))
    .map((entry) => h('span', {class: entry.state === 'unreadable' ? 'source bad' : 'source', title: entry.detail || entry.state}, `${entry.source} `, h('b', {text: entry.state === 'found' ? String(entry.jobs) : entry.state.replace('_', ' ')})));
  return h('section', {class: 'panel-block'},
    h('div', {class: 'block-head'}, h('h2', {text: hostName(host)}), h('span', {class: 'block-meta', text: plural(rows.length, 'job')})),
    h('div', {class: 'sources'}, sources),
    unreachable
      ? h('p', {class: 'block-empty', text: `Could not reach ${host}: ${access[0].detail}`})
      : rows.length ? cronTable(rows) : h('p', {class: 'block-empty', text: 'No scheduled jobs here.'}));
}
function cronTable(rows) {
  const now = Date.now();
  const tbody = h('tbody', {});
  for (const job of rows) {
    const key = cronKey(job);
    const open = store.openRows.has(key);
    const toggle = () => toggleRow(key);
    const next = cronFires(job, now)?.fires?.[0];
    const paused = ['disabled', 'paused', 'not_loaded'].includes(job.state);
    const schedule = describeSchedule(job);
    const later = !paused && !next && job.state !== 'service' ? nextFire(job, now) : null;
    const nextText = paused ? 'Paused' : next ? until(next - now) : later ? weekdayClock(later) : '-';
    const lastText = job.last_run ? ago(job.last_run) : '-';
    tbody.append(h('tr', {id: key, class: cronNeedsAttention(job) ? 'row trouble' : paused ? 'row paused' : 'row', tabindex: '0', 'aria-expanded': String(open), onclick: toggle, onkeydown: (event) => { if (event.key === 'Enter' || event.key === ' ') { event.preventDefault(); toggle(); } }},
      h('td', {class: 'ident'},
        h('span', {class: 'ident-name', text: job.name || job.id}),
        h('span', {class: 'ident-sub', text: job.name && job.name !== job.id ? `${job.source} · ${job.id}` : job.source}),
        h('span', {class: 'ident-phone', text: `${schedule} · ${nextText === '-' ? `ran ${lastText}` : nextText}`})),
      h('td', {class: 'nowrap', text: schedule}),
      h('td', {class: paused ? 'faint nowrap fig' : 'nowrap fig', title: next ? new Date(next).toLocaleString() : '', text: nextText}),
      h('td', {class: 'muted nowrap fig', title: job.last_run || '', text: lastText}),
      h('td', {class: 'result'}, cronResult(job)),
      h('td', {class: 'num', text: job.runs != null ? job.runs.toLocaleString('en-US') : '-'})));
    if (open) {
      const fields = [['ID', job.id], ['Command', job.command], ['Log', job.log && tilde(job.log)], ['Schedule', job.schedule], ['Time zone', job.tz], ['Last run', job.last_run], ['Created by', job.created_by], ['Connectors', (job.connectors || []).join(', ')], ['Effect', job.effect], ['Inferred', (job.inferred || []).join(', ')]]
        .filter(([, value]) => value);
      tbody.append(h('tr', {class: 'detail'}, h('td', {colspan: '6'}, h('div', {class: 'detail-grid'},
        (job.problems || []).length ? h('ul', {class: 'problems'}, job.problems.map((problem) => h('li', {text: sentence(grouped(problem))}))) : null,
        h('dl', {}, fields.flatMap(([label, value]) => [h('dt', {text: label}), h('dd', {class: ['ID', 'Command', 'Log', 'Schedule'].includes(label) ? 'code' : '', text: value})]))))));
    }
  }
  return h('div', {class: 'block-body'}, h('table', {class: 'grid-table jobs'},
    h('thead', {}, h('tr', {}, [['Job', ''], ['Schedule', ''], ['Next run', ''], ['Last run', ''], ['Result', ''], ['Runs', 'num']].map(([label, cls]) => h('th', {scope: 'col', class: cls, text: label})))),
    tbody));
}

// ---------- config ----------
async function loadConfigLayer(layer) {
  store.configLayer = layer;
  store.configDiff = '';
  store.configNote = '';
  store.configLoading = true;
  try { store.configState = await api(`/api/state?layer=${encodeURIComponent(layer)}`); }
  catch (error) { store.configState = null; store.configNote = error.message; }
  store.configLoading = false;
  renderView();
}
function renderConfig(root) {
  const state = store.configState;
  if (!state || state.active_layer !== store.configLayer) {
    if (store.configNote) {
      mount(root, h('div', {class: 'error-box'}, store.configNote, ' ', h('button', {type: 'button', onclick: () => loadConfigLayer(store.configLayer), text: 'Retry'})));
      return;
    }
    mount(root, h('p', {class: 'loading', text: 'Loading config…'}));
    if (!store.configLoading) loadConfigLayer(store.configLayer);
    return;
  }
  const layer = store.configLayer;
  const readOnly = state.read_only;
  const original = state.raw_yaml || '';
  const draft = store.drafts[layer] ?? original;
  const editor = h('textarea', {class: 'editor', id: `editor-${layer}`, spellcheck: 'false', 'aria-label': `${layer} YAML`, readonly: readOnly || undefined, oninput: (event) => {
    store.drafts[layer] = event.target.value;
    store.configDiff = '';
    actions.querySelectorAll('[data-dirty]').forEach((node) => { node.disabled = event.target.value === original; });
  }});
  editor.value = draft;
  const dirty = draft !== original;
  const actions = h('div', {class: 'editor-actions'},
    h('button', {type: 'button', dataset: {dirty: '1'}, disabled: readOnly || !dirty || undefined, onclick: () => validateConfig(layer), text: 'Check changes'}),
    h('button', {type: 'button', class: 'primary', dataset: {dirty: '1'}, disabled: readOnly || !dirty || undefined, onclick: () => saveConfig(layer), text: 'Save'}),
    h('button', {type: 'button', class: 'quiet', dataset: {dirty: '1'}, disabled: readOnly || !dirty || undefined, onclick: () => { delete store.drafts[layer]; store.configDiff = ''; store.configNote = ''; renderView(); }, text: 'Discard'}),
    store.configNote ? h('span', {class: 'note', text: store.configNote}) : null);
  const path = layer === 'effective' ? 'merge of both files' : tilde(state.paths?.[layer] || '');
  mount(root, 
    viewHead('Config', 'Your canonical config. Shared is tackroom.yaml in your config root; Local overrides it on this machine only; Effective is what sync uses.',
      segmented([['shared', 'Shared'], ['local', 'Local'], ['effective', 'Effective']], layer, (value) => loadConfigLayer(value))),
    h('div', {class: 'config-meta'}, h('span', {text: path}), state.revision ? h('span', {text: `revision ${state.revision.slice(0, 12)}`}) : null, readOnly ? h('span', {text: 'read-only'}) : null),
    editor,
    readOnly ? null : actions,
    store.configDiff ? renderDiff(store.configDiff) : null);
}
function renderDiff(text) {
  return h('pre', {class: 'diff', 'aria-label': 'Changes'}, text.split('\n').map((line) => {
    const kind = line.startsWith('@@') ? 'hunk' : line.startsWith('+') && !line.startsWith('+++') ? 'add' : line.startsWith('-') && !line.startsWith('---') ? 'del' : '';
    return h('span', {class: kind}, `${tilde(line)}\n`);
  }));
}
async function validateConfig(layer) {
  try {
    const result = await api('/api/config/validate', {method: 'POST', body: JSON.stringify({layer, raw_yaml: store.drafts[layer]})});
    store.configDiff = result.diff || '';
    store.configNote = result.diff ? 'Valid. Review the changes, then save.' : 'Valid, and identical to the file on disk.';
  } catch (error) { store.configDiff = ''; store.configNote = error.message; }
  renderView();
}
async function saveConfig(layer) {
  try {
    await api('/api/config/raw', {method: 'PUT', body: JSON.stringify({layer, expected_revision: store.configState.revision, raw_yaml: store.drafts[layer]})});
    delete store.drafts[layer];
    toast(`Saved ${layer === 'local' ? 'tackroom.local.yaml' : 'tackroom.yaml'}. Sync to apply it.`);
    await loadConfigLayer(layer);
    await refresh();
  } catch (error) {
    store.configNote = error.code === 'stale_revision' ? 'The file changed on disk since you opened it. Discard your edit or copy it before reloading.' : error.message;
    renderView();
  }
}

// ---------- sync ----------
const PLAN_GROUPS = [
  ['Adds', 'add', 'skill'], ['AddsAgent', 'add', 'role'], ['AddsMCP', 'add', 'MCP'], ['AddsHook', 'add', 'hook'],
  ['Updates', 'update', 'skill'], ['UpdatesAgent', 'update', 'role (overwrite)'], ['UpdatesMCP', 'update', 'MCP'], ['UpdatesHook', 'update', 'hook'], ['UpdatesPackage', 'update', 'package'],
  ['Removes', 'remove', 'skill'], ['RemovesAgent', 'remove', 'role'], ['RemovesPackage', 'remove', 'package'],
];
async function openSync() {
  const panel = document.getElementById('sync-panel');
  if (!panel.open) panel.showModal();
  store.preview = null;
  setApply(false, '');
  mount(document.getElementById('sync-body'), h('p', {class: 'loading', text: 'Building the sync plan…'}));
  try {
    store.preview = await api('/api/sync/preview', {method: 'POST', body: '{}'});
    renderPlan();
  } catch (error) {
    mount(document.getElementById('sync-body'), h('div', {class: 'error-box', text: error.message}));
  }
}
function renderPlan() {
  const plan = store.preview.plan || {};
  const body = [];
  const repoState = plan.repo?.State;
  if (repoState && repoState !== 'synced') body.push(h('div', {class: 'plan-agent'}, h('h3', {text: '~/.agents'}), h('ul', {}, h('li', {}, h('span', {class: 'verb update', text: 'link'}), h('span', {text: `${repoState} → ${tilde(plan.repo.ExpectedTarget)}`})))));
  for (const report of plan.reports || []) {
    const items = [];
    if (report.Error) items.push(['note', `config unreadable, skipped: ${report.Error}`]);
    else if (!report.Detected) items.push(['note', 'not installed, skipped']);
    else {
      for (const [key, verb, noun] of PLAN_GROUPS) for (const name of report[key] || []) items.push([verb, `${noun} ${name}`]);
      for (const conflict of report.Conflicts || []) items.push(['conflict', `${tilde(conflict)} (left alone)`]);
      if (report.RootState && report.RootState !== 'synced') items.push([report.RootState === 'missing' ? 'add' : 'update', `root instructions (${report.RootState})`]);
      if (!items.length) items.push(['note', report.Synced ? 'up to date' : 'refresh native config']);
    }
    body.push(h('div', {class: 'plan-agent'}, h('h3', {text: report.Name}), h('ul', {}, items.map(([verb, text]) => h('li', {}, h('span', {class: `verb ${verb}`, text: verb}), h('span', {text}))))));
  }
  const destructive = plan.destructive || [];
  if (destructive.length) {
    body.unshift(h('div', {class: 'destructive'},
      h('strong', {text: `${plural(destructive.length, 'change')} cannot be undone by sync. Tick each one to continue.`}),
      destructive.map((item) => h('label', {}, h('input', {type: 'checkbox', 'aria-label': `Confirm: ${item}`, dataset: {destructive: item}, onchange: updateApply}), h('span', {text: item})))));
  }
  mount(document.getElementById('sync-body'), ...body);
  updateApply();
}
function updateApply() {
  const boxes = [...document.querySelectorAll('[data-destructive]')];
  const ready = boxes.every((box) => box.checked);
  setApply(ready, ready ? (boxes.length ? 'All destructive changes confirmed.' : 'Sync writes only what tackroom manages.') : `${boxes.filter((box) => !box.checked).length} left to confirm.`);
}
function setApply(enabled, note, bad = false) {
  document.getElementById('apply-sync').disabled = !enabled;
  const node = document.getElementById('sync-note');
  node.textContent = note;
  node.className = bad ? 'note bad' : 'note';
}
async function applySync() {
  const preview = store.preview;
  if (!preview) return;
  setApply(false, 'Syncing…');
  try {
    await api('/api/sync/apply', {method: 'POST', body: JSON.stringify({expected_revision: preview.revision, plan_digest: preview.digest, confirmed_destructive: preview.plan?.destructive || []})});
    document.getElementById('sync-panel').close();
    toast('Synced to your agents.');
    await refresh();
  } catch (error) {
    if (error.code === 'sync_plan_changed' || error.code === 'stale_revision') {
      await openSync();
      setApply(false, 'Something changed since the preview. Review the new plan.', true);
      return;
    }
    setApply(true, error.message, true);
  }
}

// ---------- boot ----------
document.getElementById('open-sync').addEventListener('click', openSync);
document.getElementById('close-sync').addEventListener('click', () => document.getElementById('sync-panel').close());
document.getElementById('cancel-sync').addEventListener('click', () => document.getElementById('sync-panel').close());
document.getElementById('apply-sync').addEventListener('click', applySync);
document.getElementById('sync-panel').addEventListener('click', (event) => { if (event.target === event.currentTarget) event.currentTarget.close(); });
document.addEventListener('click', (event) => {
  const menu = document.getElementById('links-menu');
  if (menu.open && !menu.contains(event.target)) menu.open = false;
});
document.getElementById('open-nav').addEventListener('click', (event) => {
  const open = document.body.classList.toggle('nav-open');
  event.currentTarget.setAttribute('aria-expanded', String(open));
});
window.addEventListener('hashchange', route);
route();
refresh();
loadCrons();
