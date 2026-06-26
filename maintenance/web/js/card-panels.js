/* eslint-disable */
(function (M) {
'use strict';
  var UPDATE_LOG_ROLLBACK_WARNING_HTML = '<span class="update-warning-title">⚠ 최근 업데이트 실패·롤백</span><br><span class="update-warning-desc">위 기록에서 failed 또는 rollback 항목을 확인하세요.</span>';
  var activeLogPollVersion = '';
  var configModalContext = null;

  function fetchServiceStatus(cardEl, ip) {
    var summary = cardEl && cardEl.querySelector('.service-status-summary');
    if (!summary) return;
    var url = M.API_BASE + '/service-status';
    if (ip) {
      url += '?ip=' + encodeURIComponent(ip);
    }
    fetch(url)
      .then(function (res) { return res.json(); })
      .then(function (body) {
        if (body.status === 'success' && body.data && body.data.output) {
          var output = body.data.output;
          var active = M.parseActiveFromOutput(output);
          var label = active ? '[정상 서비스 상태]' : '[서비스 중지 상태]';
          M.updateStatusUI(cardEl, output, label);
        } else {
          M.updateStatusUI(cardEl, body.data || '상태를 불러올 수 없습니다.', body.data || '상태를 불러올 수 없습니다.');
        }
      })
      .catch(function () {
        M.updateStatusUI(cardEl, null, '상태를 불러올 수 없습니다.');
      });
  }

  function escapeHtml(s) {
    if (s == null) return '';
    const t = document.createElement('div');
    t.textContent = s;
    return t.innerHTML;
  }

  function fetchUpdateLogJSON(ip) {
    var url = M.API_BASE + '/update-log';
    if (ip) url += '?ip=' + encodeURIComponent(ip);
    url += (url.indexOf('?') >= 0 ? '&' : '?') + '_=' + Date.now();
    return fetch(url, { cache: 'no-store' }).then(function (res) {
      return res.json();
    });
  }

  /** API tail is oldest-first; UI shows newest-first (reversed lines). */

  function formatUpdateLogDisplay(output) {
    if (output === undefined || output === null) return '(비어 있음)';
    var s = String(output);
    if (s === '') return '(비어 있음)';
    if (s.indexOf('\n') === -1) return s;
    var lines = s.split('\n');
    if (lines.length > 1 && lines[lines.length - 1] === '') lines.pop();
    lines.reverse();
    return lines.join('\n');
  }

  /**
   * Log file is append-newest-at-bottom. Complete when we've seen this run's started, then the last line is success/failed.
   */

  function updateLogRunComplete(output, version, runStartedSeen) {
    if (!output || !version || !runStartedSeen) return false;
    var lines = output.split('\n');
    var last = '';
    for (var i = lines.length - 1; i >= 0; i--) {
      if (lines[i].trim()) {
        last = lines[i];
        break;
      }
    }
    var needleSuccess = 'update ' + version + ' success';
    var needleFailed = 'update ' + version + ' failed';
    return last.indexOf(needleSuccess) !== -1 || last.indexOf(needleFailed) !== -1;
  }

  /** 2s poll until this run's success/failed (independent of /self; not stopped when host responds). */

  function startUpdateLogPolling(version, cardEl, ip) {
    if (!version) return function () {};
    var intervalMs = 2000;
    var maxMs = intervalMs * 450;
    var timer = null;
    var maxTimer = null;
    var needleStarted = 'update ' + version + ' started';
    var runStartedSeen = false;
    activeLogPollVersion = version;

    function stop() {
      activeLogPollVersion = '';
      if (timer) { clearInterval(timer); timer = null; }
      if (maxTimer) { clearTimeout(maxTimer); maxTimer = null; }
    }

    function tick() {
      var pre;
      var warningEl;
      if (ip) {
        if (!cardEl) return;
        pre = cardEl.querySelector('.card-right-log-output');
        warningEl = cardEl.querySelector('.card-update-rollback-warning');
      } else {
        pre = M.el('self-update-log-output');
        warningEl = M.el('self-update-rollback-warning');
      }
      if (!pre) return;
      fetchUpdateLogJSON(ip)
        .then(function (body) {
          if (warningEl) warningEl.hidden = true;
          applyUpdateLogResponse(pre, warningEl, body);
          if (body.status === 'success' && body.data && body.data.output) {
            var out = body.data.output;
            if (out.indexOf(needleStarted) !== -1) runStartedSeen = true;
            if (updateLogRunComplete(out, version, runStartedSeen)) {
              stop();
            }
          }
        })
        .catch(function () {});
    }

    tick();
    timer = setInterval(tick, intervalMs);
    maxTimer = setTimeout(stop, maxMs);
    return stop;
  }

  function extractUpdateLogOutput(data) {
    if (data === undefined || data === null) return '';
    if (typeof data === 'string') return data;
    if (typeof data === 'object' && data.output !== undefined && data.output !== null) {
      return String(data.output);
    }
    return '';
  }

  function applyUpdateLogResponse(pre, warningEl, body) {
    if (body.status === 'success' && body.data !== undefined && body.data !== null) {
      pre.textContent = formatUpdateLogDisplay(extractUpdateLogOutput(body.data));
      if (warningEl && typeof body.data === 'object' && body.data.recent_rollback) {
        warningEl.hidden = false;
        warningEl.innerHTML = UPDATE_LOG_ROLLBACK_WARNING_HTML;
      }
    } else {
      pre.textContent = body.data || '로그를 불러올 수 없습니다.';
    }
  }

  function formatMemory(host) {
    if (host.memory_total_mb != null && host.memory_used_mb != null) {
      const pct = host.memory_usage_percent != null ? host.memory_usage_percent.toFixed(1) + '%' : '';
      return host.memory_used_mb + ' / ' + host.memory_total_mb + ' MB' + (pct ? ' (' + pct + ')' : '');
    }
    return '-';
  }

  function updateHostCardDetails(cardEl, host) {
    if (!cardEl || !host) return;
    cardEl.setAttribute('data-host-version', host.version || '');
    if (host.build_variant != null && String(host.build_variant).trim() !== '') {
      cardEl.setAttribute('data-build-variant', M.defaultAgentVariantFromBuild(host.build_variant));
    }
    cardEl.setAttribute('data-hostname', host.hostname || '');
    var existingIps = (cardEl.getAttribute('data-host-ips') || '').trim();
    var ipDisplay;
    var ipsAttr;
    var primaryIp;
    if (existingIps.indexOf(',') !== -1) {
      ipsAttr = existingIps;
      ipDisplay = existingIps.split(',').map(function (s) { return s.trim(); }).filter(Boolean).join(', ');
      primaryIp = cardEl.getAttribute('data-host-ip') || host.host_ip || '';
    } else {
      ipDisplay = (host.host_ips && host.host_ips.length) ? host.host_ips.join(', ') : (host.host_ip || '-');
      ipsAttr = (host.host_ips && host.host_ips.length) ? host.host_ips.join(',') : (host.host_ip || '');
      primaryIp = host.host_ip || (host.host_ips && host.host_ips[0]) || '';
    }
    cardEl.setAttribute('data-host-ip', primaryIp);
    cardEl.setAttribute('data-host-ips', ipsAttr);
    if (host.responded_from_ip) {
      var rf = (cardEl.getAttribute('data-responded-from-ips') || '').split(',').map(function (s) { return s.trim(); }).filter(Boolean);
      if (rf.indexOf(host.responded_from_ip) === -1) rf.push(host.responded_from_ip);
      cardEl.setAttribute('data-responded-from-ips', rf.join(','));
    }
    var respondedFromDisplay = (cardEl.getAttribute('data-responded-from-ips') || '').split(',').map(function (s) { return s.trim(); }).filter(Boolean).join(', ') || '-';
    var dds = cardEl.querySelectorAll('.host-details > dd');
    if (dds.length >= 8) {
      dds[0].textContent = host.cpu_uuid || '-';
      dds[1].innerHTML = escapeHtml(host.version || '-') + (host.build_variant ? ' <span class="build-variant-badge">(' + escapeHtml(host.build_variant) + ')</span>' : '');
      dds[2].textContent = ipDisplay;
      dds[3].textContent = respondedFromDisplay;
      dds[4].textContent = host.hostname || '-';
      dds[5].textContent = host.service_port != null ? host.service_port : '-';
      dds[6].innerHTML = escapeHtml(host.cpu_info || '-') + (host.cpu_usage_percent != null ? ' (' + host.cpu_usage_percent.toFixed(1) + '%)' : '');
      dds[7].textContent = formatMemory(host);
    }
    var row = cardEl.closest && cardEl.closest('.host-row');
    if (row) M.updateHostRowLabel(row, host, cardEl.classList.contains('self-card'));
    var variantSel = cardEl.querySelector('.card-variant-selector');
    if (variantSel && !variantSel.hidden) {
      M.setVariantRadioSelection(variantSel, cardEl.getAttribute('data-build-variant'));
    }
    if (cardEl.classList.contains('self-card')) {
      M.applyLocalVariantDefault();
    }
  }

  function refreshHostCardDetails(cardEl, ip) {
    if (!cardEl) return;
    var url = (ip === '') ? (M.API_BASE + '/self') : (M.API_BASE + '/host-info?ip=' + encodeURIComponent(ip));
    fetch(url)
      .then(function (res) { return res.json(); })
      .then(function (body) {
        if (body.status === 'success' && body.data) {
          updateHostCardDetails(cardEl, body.data);
          if (ip !== '') M.updateAllHostApplyButtons();
        }
      })
      .catch(function () {});
  }

  function showCardUpdating(card, show) {
    if (!card) return;
    card.classList.toggle('is-updating', !!show);
  }

  function findHostCardByIp(container, ip) {
    if (!container || !ip) return null;
    var cards = container.querySelectorAll('.host-card[data-host-ip]');
    for (var i = 0; i < cards.length; i++) {
      var c = cards[i];
      if (c.getAttribute('data-host-ip') === ip) return c;
      var ips = (c.getAttribute('data-host-ips') || '').split(',').map(function (s) { return s.trim(); }).filter(Boolean);
      if (ips.indexOf(ip) !== -1) return c;
    }
    return null;
  }

  function findHostCardByCpuUuid(container, cpuUuid) {
    if (!container || !cpuUuid) return null;
    var cards = container.querySelectorAll('.host-card[data-cpu-uuid]');
    for (var i = 0; i < cards.length; i++) {
      if (cards[i].getAttribute('data-cpu-uuid') === cpuUuid) return cards[i];
    }
    return null;
  }

  function findHostCardForBulkProgressResult(list, r) {
    if (!list || !r) return null;
    var tryIps = [];
    if (r.connect_ip) tryIps.push(r.connect_ip);
    if (r.ip) tryIps.push(r.ip);
    for (var i = 0; i < tryIps.length; i++) {
      var byIp = findHostCardByIp(list, tryIps[i]);
      if (byIp) return byIp;
    }
    if (r.cpu_uuid) return findHostCardByCpuUuid(list, r.cpu_uuid);
    return null;
  }

  function resolveConfigContext(cardEl, ip) {
    if (!cardEl || ip === undefined || ip === null || ip === '') {
      return {
        cardEl: document.querySelector('#self-info .host-card') || null,
        ip: '',
        editor: M.el('self-current-config-editor'),
        statusEl: M.el('self-current-config-status')
      };
    }
    return {
      cardEl: cardEl,
      ip: ip,
      editor: cardEl.querySelector('.card-right-config-editor'),
      statusEl: cardEl.querySelector('.card-current-config-status')
    };
  }

  var configModalContext = null;

  function fetchCurrentConfigForContext(ctx, targetEditor, targetStatusEl) {
    var editor = targetEditor || ctx.editor;
    var statusEl = targetStatusEl !== undefined ? targetStatusEl : ctx.statusEl;
    if (!editor) return Promise.resolve();
    if (statusEl) statusEl.textContent = '';
    editor.placeholder = '불러오는 중…';
    var url = M.API_BASE + '/current-config';
    if (ctx.ip) url += '?ip=' + encodeURIComponent(ctx.ip);
    return fetch(url)
      .then(function (res) { return res.json(); })
      .then(function (body) {
        editor.placeholder = '불러오기로 current 버전의 agent.local.yml을 불러옵니다.';
        if (body.status === 'success' && body.data && body.data.content !== undefined) {
          editor.value = body.data.content;
          if (ctx.editor && editor !== ctx.editor) ctx.editor.value = body.data.content;
          if (statusEl) statusEl.textContent = '불러왔습니다.';
        } else {
          editor.value = '';
          if (ctx.editor && editor !== ctx.editor) ctx.editor.value = '';
          if (statusEl) statusEl.textContent = body.data || '불러오기 실패.';
        }
      })
      .catch(function () {
        editor.placeholder = '불러오기로 current 버전의 agent.local.yml을 불러옵니다.';
        editor.value = '';
        if (ctx.editor && editor !== ctx.editor) ctx.editor.value = '';
        if (statusEl) statusEl.textContent = '불러오기 실패.';
      });
  }

  function saveCurrentConfigForContext(ctx, content, targetStatusEl) {
    var statusEl = targetStatusEl !== undefined ? targetStatusEl : ctx.statusEl;
    var payload = { content: content !== undefined ? content : (ctx.editor ? ctx.editor.value : '') };
    if (ctx.ip) payload.ip = ctx.ip;
    if (statusEl) statusEl.textContent = '저장 중…';
    return fetch(M.API_BASE + '/current-config', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(payload)
    })
      .then(function (res) { return res.json(); })
      .then(function (body) {
        if (body.status === 'success' && ctx.editor && content !== undefined) {
          ctx.editor.value = content;
        }
        if (statusEl) {
          statusEl.textContent = body.status === 'success' ? '저장했습니다.' : (body.data || '저장 실패.');
        }
        return body;
      })
      .catch(function () {
        if (statusEl) statusEl.textContent = '저장 요청 실패.';
      });
  }

  function openConfigEditorModal(cardEl, ip) {
    configModalContext = { cardEl: cardEl || null, ip: ip || '' };
    var ctx = resolveConfigContext(cardEl, ip);
    var modal = M.el('config-editor-modal');
    var modalEditor = M.el('config-editor-modal-textarea');
    var title = M.el('config-editor-modal-title');
    var modalStatus = M.el('config-editor-modal-status');
    if (title) {
      title.textContent = ip ? ('agent.local.yml (current) — ' + ip) : 'agent.local.yml (current)';
    }
    if (modalEditor) {
      modalEditor.value = ctx.editor ? ctx.editor.value : '';
      modalEditor.placeholder = '불러오기로 current 버전의 agent.local.yml을 불러옵니다.';
    }
    if (modalStatus) modalStatus.textContent = '';
    if (modal) {
      modal.hidden = false;
      modal.setAttribute('aria-hidden', 'false');
    }
    document.body.classList.add('config-modal-open');
    if (modalEditor) modalEditor.focus();
  }

  function closeConfigEditorModal() {
    var modalEditor = M.el('config-editor-modal-textarea');
    if (configModalContext && modalEditor) {
      var ctx = resolveConfigContext(configModalContext.cardEl, configModalContext.ip);
      if (ctx.editor) ctx.editor.value = modalEditor.value;
    }
    var modal = M.el('config-editor-modal');
    if (modal) {
      modal.hidden = true;
      modal.setAttribute('aria-hidden', 'true');
    }
    document.body.classList.remove('config-modal-open');
    configModalContext = null;
  }

  function loadConfigEditorModal() {
    if (!configModalContext) return;
    var ctx = resolveConfigContext(configModalContext.cardEl, configModalContext.ip);
    fetchCurrentConfigForContext(ctx, M.el('config-editor-modal-textarea'), M.el('config-editor-modal-status'))
      .then(function () {
        var modalStatus = M.el('config-editor-modal-status');
        if (ctx.statusEl && modalStatus) ctx.statusEl.textContent = modalStatus.textContent;
      });
  }

  function saveConfigEditorModal() {
    if (!configModalContext) return;
    var ctx = resolveConfigContext(configModalContext.cardEl, configModalContext.ip);
    var modalEditor = M.el('config-editor-modal-textarea');
    var content = modalEditor ? modalEditor.value : '';
    saveCurrentConfigForContext(ctx, content, M.el('config-editor-modal-status'))
      .then(function () {
        var modalStatus = M.el('config-editor-modal-status');
        if (ctx.statusEl && modalStatus) ctx.statusEl.textContent = modalStatus.textContent;
      });
  }

  function initConfigEditorModal() {
    var modal = M.el('config-editor-modal');
    if (!modal || modal.getAttribute('data-bound')) return;
    modal.setAttribute('data-bound', '1');
    var backdrop = modal.querySelector('.config-editor-modal-backdrop');
    var closeBtn = M.el('config-editor-modal-close-btn');
    var dismissBtn = M.el('config-editor-modal-dismiss-btn');
    var loadBtn = M.el('config-editor-modal-load-btn');
    var saveBtn = M.el('config-editor-modal-save-btn');
    if (backdrop) backdrop.addEventListener('click', closeConfigEditorModal);
    if (closeBtn) closeBtn.addEventListener('click', closeConfigEditorModal);
    if (dismissBtn) dismissBtn.addEventListener('click', closeConfigEditorModal);
    if (loadBtn) loadBtn.addEventListener('click', loadConfigEditorModal);
    if (saveBtn) saveBtn.addEventListener('click', saveConfigEditorModal);
    document.addEventListener('keydown', function (e) {
      if (e.key === 'Escape' && configModalContext) closeConfigEditorModal();
    });
  }

  function fetchCurrentConfig() {
    fetchCurrentConfigForContext(resolveConfigContext(null, ''));
  }

  function saveCurrentConfig() {
    saveCurrentConfigForContext(resolveConfigContext(null, ''));
  }

  function fetchUpdateLog(silent) {
    var pre = M.el('self-update-log-output');
    var warningEl = M.el('self-update-rollback-warning');
    if (!pre) return;
    if (!silent) pre.textContent = '불러오는 중…';
    if (warningEl) warningEl.hidden = true;
    fetchUpdateLogJSON('')
      .then(function (body) { applyUpdateLogResponse(pre, warningEl, body); })
      .catch(function () {
        pre.textContent = '로그를 불러올 수 없습니다.';
      });
  }

  function fetchVersionsList() {
    var container = M.el('self-versions-list-container');
    var statusEl = M.el('self-versions-status');
    var removeBtn = M.el('self-versions-remove-btn');
    if (!container) return Promise.resolve();
    container.innerHTML = '<div class="versions-loading">불러오는 중…</div>';
    if (statusEl) statusEl.textContent = '';
    if (removeBtn) removeBtn.disabled = true;
    return fetch(M.API_BASE + '/versions/list')
      .then(function (res) { return res.json(); })
      .then(function (body) {
        if (body.status !== 'success' || !body.data || !body.data.versions) {
          container.innerHTML = '<div class="versions-loading">목록을 불러올 수 없습니다.</div>';
          return;
        }
        var versions = body.data.versions;
        if (versions.length === 0) {
          container.innerHTML = '<div class="versions-loading">설치된 버전이 없습니다.</div>';
          fillVersionsSwitchSelect(M.el('self-versions-switch-select'), []);
          setVersionsSwitchHint(null, '');
          return;
        }
        renderVersionsListIntoContainer(container, versions, null);
        var wrapper = container.querySelector('.versions-list-wrapper');
        if (wrapper) {
          wrapper.addEventListener('change', updateVersionsRemoveButtonState);
          updateVersionsRemoveButtonState();
        }
      })
      .catch(function () {
        container.innerHTML = '<div class="versions-loading">목록을 불러올 수 없습니다.</div>';
      })
      .finally(function () {
        updateVersionsSwitchButtonFromSelect(null);
        var s = M.el('self-versions-switch-select');
        setVersionsSwitchHint(null, s && s.value ? s.value : '');
      });
  }

  function syncVersionsRemoveButton(removeBtn, listContainer) {
    if (!removeBtn || !listContainer) return;
    var checked = listContainer.querySelectorAll('.versions-list-wrapper .versions-list input[type="checkbox"]:not(:disabled):checked');
    removeBtn.disabled = checked.length === 0;
  }

  function updateVersionsRemoveButtonState() {
    syncVersionsRemoveButton(M.el('self-versions-remove-btn'), M.el('self-versions-list-container'));
  }

  function updateVersionsRemoveButtonStateForCard(cardEl) {
    if (!cardEl) return;
    syncVersionsRemoveButton(
      cardEl.querySelector('.card-versions-remove-btn'),
      cardEl.querySelector('.card-right-versions-list-container'));
  }

  function fetchUpdateLogForCard(cardEl, ip) {
    if (!cardEl || !ip) return;
    var pre = cardEl.querySelector('.card-right-log-output');
    var warningEl = cardEl.querySelector('.card-update-rollback-warning');
    if (!pre) return;
    pre.textContent = '불러오는 중…';
    if (warningEl) warningEl.hidden = true;
    fetchUpdateLogJSON(ip)
      .then(function (body) { applyUpdateLogResponse(pre, warningEl, body); })
      .catch(function () {
        pre.textContent = '로그를 불러올 수 없습니다.';
      });
  }

  function fetchCurrentConfigForCard(cardEl, ip) {
    if (!cardEl || !ip) return;
    fetchCurrentConfigForContext(resolveConfigContext(cardEl, ip));
  }

  function saveCurrentConfigForCard(cardEl, ip) {
    if (!cardEl || !ip) return;
    saveCurrentConfigForContext(resolveConfigContext(cardEl, ip));
  }

  function renderVersionsListIntoContainer(container, versions, cardEl) {
    if (!container || !versions || !Array.isArray(versions)) return;
    if (versions.length === 0) {
      container.innerHTML = '<div class="versions-loading">설치된 버전이 없습니다.</div>';
      return;
    }
    var mid = Math.ceil(versions.length / 2);
    var col0 = versions.slice(0, mid);
    var col1 = versions.slice(mid);
    function makeList(part, offset) {
      var ul = document.createElement('ul');
      ul.className = 'versions-list';
      for (var i = 0; i < part.length; i++) {
        var v = part[i];
        var idx = offset + i;
        var li = document.createElement('li');
        var canDelete = !v.is_current && !v.is_previous;
        var cb = document.createElement('input');
        cb.type = 'checkbox';
        cb.id = 'versions-cb-' + (cardEl ? cardEl.getAttribute('data-host-ip') + '-' : '') + idx;
        cb.setAttribute('data-version', v.version);
        cb.disabled = !canDelete;
        var label = document.createElement('label');
        label.htmlFor = cb.id;
        label.textContent = v.version;
        var badge = document.createElement('span');
        badge.className = 'version-badge';
        if (v.is_current) badge.className += ' is-current';
        else if (v.is_previous) badge.className += ' is-previous';
        badge.textContent = v.is_current ? '현재' : (v.is_previous ? '이전' : '');
        li.appendChild(cb);
        li.appendChild(badge);
        li.appendChild(label);
        ul.appendChild(li);
      }
      return ul;
    }
    var wrapper = document.createElement('div');
    wrapper.className = 'versions-list-wrapper';
    wrapper.appendChild(makeList(col0, 0));
    wrapper.appendChild(makeList(col1, mid));
    container.innerHTML = '';
    container.appendChild(wrapper);
    if (cardEl) {
      wrapper.addEventListener('change', function () { updateVersionsRemoveButtonStateForCard(cardEl); });
      updateVersionsRemoveButtonStateForCard(cardEl);
    }
    var switchSel = cardEl ? cardEl.querySelector('.card-versions-switch-select') : M.el('self-versions-switch-select');
    fillVersionsSwitchSelect(switchSel, versions);
    updateVersionsSwitchButtonFromSelect(cardEl);
    setVersionsSwitchHint(cardEl, switchSel && switchSel.value ? switchSel.value : '');
  }

  function fillVersionsSwitchSelect(selectEl, versions) {
    if (!selectEl || !versions || !Array.isArray(versions)) return;
    selectEl.innerHTML = '';
    var z = document.createElement('option');
    z.value = '';
    z.textContent = '버전 선택…';
    selectEl.appendChild(z);
    for (var i = 0; i < versions.length; i++) {
      var v = versions[i];
      if (v.is_current) continue;
      var o = document.createElement('option');
      o.value = v.version;
      o.textContent = v.version + (v.is_previous ? ' (이전)' : '');
      selectEl.appendChild(o);
    }
  }

  function setVersionsSwitchHint(cardEl, versionKey) {
    var hint = cardEl ? cardEl.querySelector('.versions-switch-hint') : M.el('self-versions-switch-hint');
    if (!hint) return;
    hint.textContent = versionKey ? ('버전 ' + versionKey + ' 을(를) 선택했습니다.') : '';
  }

  function updateVersionsSwitchButtonFromSelect(cardEl) {
    var sel = cardEl ? cardEl.querySelector('.card-versions-switch-select') : M.el('self-versions-switch-select');
    var btn = cardEl ? cardEl.querySelector('.card-versions-switch-btn') : M.el('self-versions-switch-btn');
    if (!sel || !btn) return;
    btn.disabled = !sel.value;
  }

  function fetchVersionsListForCard(cardEl, ip) {
    if (!cardEl || !ip) return Promise.resolve();
    var container = cardEl.querySelector('.card-right-versions-list-container');
    var statusEl = cardEl.querySelector('.card-versions-status');
    var removeBtn = cardEl.querySelector('.card-versions-remove-btn');
    if (!container) return Promise.resolve();
    container.innerHTML = '<div class="versions-loading">불러오는 중…</div>';
    if (statusEl) statusEl.textContent = '';
    if (removeBtn) removeBtn.disabled = true;
    return fetch(M.API_BASE + '/versions/list?ip=' + encodeURIComponent(ip))
      .then(function (res) { return res.json(); })
      .then(function (body) {
        if (body.status !== 'success' || !body.data || !body.data.versions) {
          container.innerHTML = '<div class="versions-loading">목록을 불러올 수 없습니다.</div>';
          return;
        }
        var vers = body.data.versions;
        if (vers.length === 0) {
          container.innerHTML = '<div class="versions-loading">설치된 버전이 없습니다.</div>';
          fillVersionsSwitchSelect(cardEl.querySelector('.card-versions-switch-select'), []);
          setVersionsSwitchHint(cardEl, '');
          return;
        }
        renderVersionsListIntoContainer(container, vers, cardEl);
      })
      .catch(function () {
        container.innerHTML = '<div class="versions-loading">목록을 불러올 수 없습니다.</div>';
      })
      .finally(function () {
        updateVersionsSwitchButtonFromSelect(cardEl);
        var s = cardEl.querySelector('.card-versions-switch-select');
        setVersionsSwitchHint(cardEl, s && s.value ? s.value : '');
      });
  }

  function doVersionsRemoveForCard(cardEl, ip) {
    if (!cardEl || !ip) return;
    var container = cardEl.querySelector('.card-right-versions-list-container');
    var statusEl = cardEl.querySelector('.card-versions-status');
    var removeBtn = cardEl.querySelector('.card-versions-remove-btn');
    if (!container || !removeBtn || removeBtn.disabled) return;
    var checked = container.querySelectorAll('.versions-list-wrapper .versions-list input[type="checkbox"]:not(:disabled):checked');
    if (checked.length === 0) return;
    var versions = [];
    for (var i = 0; i < checked.length; i++) {
      versions.push(checked[i].getAttribute('data-version'));
    }
    if (statusEl) statusEl.textContent = '삭제 중…';
    removeBtn.disabled = true;
    fetch(M.API_BASE + '/versions/remove', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ versions: versions, ip: ip })
    })
      .then(function (res) { return res.json(); })
      .then(function (body) {
        if (statusEl) statusEl.textContent = body.data || (body.status === 'success' ? '삭제 요청 완료.' : '');
        fetchVersionsListForCard(cardEl, ip);
      })
      .catch(function () {
        if (statusEl) statusEl.textContent = '삭제 요청 실패.';
        if (removeBtn) removeBtn.disabled = false;
      });
  }

  function doVersionsSwitch(cardEl, ip) {
    var sel = cardEl ? cardEl.querySelector('.card-versions-switch-select') : M.el('self-versions-switch-select');
    var statusEl = cardEl ? cardEl.querySelector('.card-versions-status') : M.el('self-versions-status');
    var btn = cardEl ? cardEl.querySelector('.card-versions-switch-btn') : M.el('self-versions-switch-btn');
    if (!sel || !btn || btn.disabled) return;
    var version = sel.value;
    if (!version) return;
    var payload = { version: version };
    if (ip) payload.ip = ip;
    if (statusEl) statusEl.textContent = '전환 적용 중…';
    btn.disabled = true;
    fetch(M.API_BASE + '/versions/switch-current', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(payload)
    })
      .then(function (res) { return res.json(); })
      .then(function (body) {
        if (statusEl) {
          if (body.status === 'success') {
            statusEl.textContent = typeof body.data === 'string'
              ? body.data
              : '전환 작업이 시작되었습니다. systemd-run으로 update.sh가 실행 중이며, 완료·실패는 수십 초 내에 반영됩니다. 실패 시 업데이트 로그를 확인하세요.';
          } else {
            statusEl.textContent = (typeof body.data === 'string' && body.data) ? body.data : '전환 실패.';
          }
        }
        if (body.status === 'success') {
          M.scheduleRefreshAfterSwitchCurrent(cardEl, ip, version, statusEl);
        } else if (btn) {
          btn.disabled = false;
        }
      })
      .catch(function () {
        if (statusEl) statusEl.textContent = '요청 실패.';
        if (btn) btn.disabled = false;
      });
  }

  function doVersionsRemove() {
    var container = M.el('self-versions-list-container');
    var statusEl = M.el('self-versions-status');
    var removeBtn = M.el('self-versions-remove-btn');
    if (!container || !removeBtn || removeBtn.disabled) return;
    var checked = container.querySelectorAll('.versions-list-wrapper .versions-list input[type="checkbox"]:not(:disabled):checked');
    if (checked.length === 0) return;
    var versions = [];
    for (var i = 0; i < checked.length; i++) {
      versions.push(checked[i].getAttribute('data-version'));
    }
    if (statusEl) statusEl.textContent = '삭제 중…';
    removeBtn.disabled = true;
    fetch(M.API_BASE + '/versions/remove', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ versions: versions })
    })
      .then(function (res) { return res.json(); })
      .then(function (body) {
        if (statusEl) statusEl.textContent = body.data || (body.status === 'success' ? '삭제 요청 완료.' : '');
        fetchVersionsList();
      })
      .catch(function () {
        if (statusEl) statusEl.textContent = '요청 실패.';
        fetchVersionsList();
      });
  }

  Object.defineProperty(M, 'activeLogPollVersion', {
    get: function () { return activeLogPollVersion; },
    set: function (v) { activeLogPollVersion = v; },
    configurable: true
  });

  // exports
  M.applyUpdateLogResponse = applyUpdateLogResponse;
  M.closeConfigEditorModal = closeConfigEditorModal;
  M.doVersionsRemove = doVersionsRemove;
  M.doVersionsRemoveForCard = doVersionsRemoveForCard;
  M.doVersionsSwitch = doVersionsSwitch;
  M.escapeHtml = escapeHtml;
  M.extractUpdateLogOutput = extractUpdateLogOutput;
  M.fetchCurrentConfig = fetchCurrentConfig;
  M.fetchCurrentConfigForCard = fetchCurrentConfigForCard;
  M.fetchCurrentConfigForContext = fetchCurrentConfigForContext;
  M.fetchServiceStatus = fetchServiceStatus;
  M.fetchUpdateLog = fetchUpdateLog;
  M.fetchUpdateLogForCard = fetchUpdateLogForCard;
  M.fetchUpdateLogJSON = fetchUpdateLogJSON;
  M.fetchVersionsList = fetchVersionsList;
  M.fetchVersionsListForCard = fetchVersionsListForCard;
  M.fillVersionsSwitchSelect = fillVersionsSwitchSelect;
  M.findHostCardByCpuUuid = findHostCardByCpuUuid;
  M.findHostCardByIp = findHostCardByIp;
  M.findHostCardForBulkProgressResult = findHostCardForBulkProgressResult;
  M.formatMemory = formatMemory;
  M.formatUpdateLogDisplay = formatUpdateLogDisplay;
  M.initConfigEditorModal = initConfigEditorModal;
  M.loadConfigEditorModal = loadConfigEditorModal;
  M.openConfigEditorModal = openConfigEditorModal;
  M.refreshHostCardDetails = refreshHostCardDetails;
  M.renderVersionsListIntoContainer = renderVersionsListIntoContainer;
  M.resolveConfigContext = resolveConfigContext;
  M.saveConfigEditorModal = saveConfigEditorModal;
  M.saveCurrentConfig = saveCurrentConfig;
  M.saveCurrentConfigForCard = saveCurrentConfigForCard;
  M.saveCurrentConfigForContext = saveCurrentConfigForContext;
  M.setVersionsSwitchHint = setVersionsSwitchHint;
  M.showCardUpdating = showCardUpdating;
  M.startUpdateLogPolling = startUpdateLogPolling;
  M.syncVersionsRemoveButton = syncVersionsRemoveButton;
  M.updateHostCardDetails = updateHostCardDetails;
  M.updateLogRunComplete = updateLogRunComplete;
  M.updateVersionsRemoveButtonState = updateVersionsRemoveButtonState;
  M.updateVersionsRemoveButtonStateForCard = updateVersionsRemoveButtonStateForCard;
  M.updateVersionsSwitchButtonFromSelect = updateVersionsSwitchButtonFromSelect;
})(window.MolMaintenance = window.MolMaintenance || {});
