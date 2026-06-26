/* eslint-disable */
(function (M) {
'use strict';
  /** After bulk apply-update: poll host-info like per-card apply (version, log, panels). */
  function refreshRemoteCardsAfterBulkApplyUpdate(progressResults, appliedVersion) {
    var list = M.el('discovered-hosts');
    if (!list || !progressResults || !progressResults.length) return;
    var refreshed = {};
    for (var i = 0; i < progressResults.length; i++) {
      var r = progressResults[i];
      if (r.status !== 'success') continue;
      var card = M.findHostCardForBulkProgressResult(list, r);
      if (!card) continue;
      var ip = card.getAttribute('data-host-ip') || r.connect_ip || r.ip || '';
      if (!ip || refreshed[ip]) continue;
      refreshed[ip] = true;
      M.showCardUpdating(card, true);
      M.scheduleRefreshAfterApply(
        card,
        ip,
        card.querySelector('.service-status-summary'),
        '업데이트 반영됨.',
        r.version || appliedVersion,
        function () {
          refreshRemoteBulkButtonsState();
        },
        { skipInitialSummary: true }
      );
    }
  }

  function mergeHostIpsIntoCard(cardEl, newIp) {
    if (!cardEl || !newIp) return;
    var ips = (cardEl.getAttribute('data-host-ips') || '').split(',').map(function (s) { return s.trim(); }).filter(Boolean);
    if (ips.indexOf(newIp) === -1) ips.push(newIp);
    cardEl.setAttribute('data-host-ips', ips.join(','));
    var dds = cardEl.querySelectorAll('.host-details > dd');
    if (dds.length >= 8) dds[2].textContent = ips.join(', ');
  }

  function mergeHostIpsFromResponseIntoCard(cardEl, host) {
    if (!cardEl || !host) return;
    if (host.host_ips && host.host_ips.length) {
      for (var i = 0; i < host.host_ips.length; i++) mergeHostIpsIntoCard(cardEl, host.host_ips[i]);
    } else if (host.host_ip) {
      mergeHostIpsIntoCard(cardEl, host.host_ip);
    }
  }

  function mergeRespondedFromIntoCard(cardEl, newRespondedFromIp) {
    if (!cardEl || !newRespondedFromIp) return;
    var ips = (cardEl.getAttribute('data-responded-from-ips') || '').split(',').map(function (s) { return s.trim(); }).filter(Boolean);
    if (ips.indexOf(newRespondedFromIp) === -1) ips.push(newRespondedFromIp);
    cardEl.setAttribute('data-responded-from-ips', ips.join(','));
    var dds = cardEl.querySelectorAll('.host-details > dd');
    if (dds.length >= 8) dds[3].textContent = ips.join(', ');
  }

  var PUSH_CONFIG_ALL_LABEL = '로컬 설정을 리모트 호스트에 일괄 복사';
  var RESTART_ALL_LABEL = '리모트 호스트 일괄 재시작';
  var APPLY_UPDATE_ALL_LABEL = '리모트 호스트에 일괄 업데이트 적용';
  var ROLLBACK_ALL_LABEL = '리모트 호스트 일괄 롤백';
  var PUSH_CONFIG_ALL_STATUS_LABEL = '설정 복사';
  var RESTART_ALL_STATUS_LABEL = '서비스 재시작';
  var APPLY_UPDATE_ALL_STATUS_LABEL = '업데이트 적용';
  var ROLLBACK_ALL_STATUS_LABEL = '롤백';

  function countRemoteHostCards() {
    var list = M.el('discovered-hosts');
    if (!list) return 0;
    return list.querySelectorAll('.host-card:not(.self-card)').length;
  }

  function collectRemoteHostsFromDOM() {
    var list = M.el('discovered-hosts');
    if (!list) return [];
    var cards = list.querySelectorAll('.host-card:not(.self-card)');
    var hosts = [];
    for (var i = 0; i < cards.length; i++) {
      var card = cards[i];
      var primary = (card.getAttribute('data-host-ip') || '').trim();
      var seen = {};
      var ips = [];
      function addIp(ip) {
        ip = (ip || '').trim();
        if (ip && !seen[ip]) {
          seen[ip] = true;
          ips.push(ip);
        }
      }
      addIp(primary);
      (card.getAttribute('data-host-ips') || '').split(',').forEach(function (s) { addIp(s); });
      (card.getAttribute('data-responded-from-ips') || '').split(',').forEach(function (s) { addIp(s); });
      if (ips.length === 0) continue;
      if (!primary) primary = ips[0];
      hosts.push({
        primary_ip: primary,
        hostname: (card.getAttribute('data-hostname') || '').trim(),
        cpu_uuid: (card.getAttribute('data-cpu-uuid') || '').trim(),
        ips: ips
      });
    }
    return hosts;
  }

  function formatBulkLogTimestamp(date) {
    if (!date) date = new Date();
    if (!(date instanceof Date)) date = new Date(date);
    function pad(n) { return (n < 10 ? '0' : '') + n; }
    return '[' + date.getFullYear() + '-' + pad(date.getMonth() + 1) + '-' + pad(date.getDate()) +
      ' ' + pad(date.getHours()) + ':' + pad(date.getMinutes()) + ':' + pad(date.getSeconds()) + ']';
  }

  function prefixBulkStatusMessage(operationLabel, message, at) {
    var ts = at ? formatBulkLogTimestamp(at) + ' ' : '';
    var prefix = ts + '「' + (operationLabel || '일괄 작업') + '」 ';
    if (!message) return prefix.trim();
    return prefix + message;
  }

  function formatBulkResultLogLine(at, text) {
    return formatBulkLogTimestamp(at) + ' ' + text;
  }

  function createBulkRemoteStatusEntry(operationLabel, resultsTitle) {
    var list = M.el('bulk-remote-status-list');
    if (!list) return null;
    var startedAt = new Date();
    var resultsStore = { lines: [], title: resultsTitle || '결과', startedAt: startedAt };
    var row = document.createElement('div');
    row.className = 'bulk-remote-status-row';
    row.innerHTML =
      '<p class="discovery-status bulk-remote-status-text" aria-live="polite"></p>' +
      '<div class="bulk-remote-status-actions">' +
      '<button type="button" class="service-btn bulk-remote-entry-results-btn" hidden>결과 보기</button>' +
      '<button type="button" class="bulk-remote-status-dismiss" hidden title="메시지 닫기" aria-label="메시지 닫기">&times;</button>' +
      '</div>';
    var statusEl = row.querySelector('.bulk-remote-status-text');
    var resultsBtn = row.querySelector('.bulk-remote-entry-results-btn');
    var dismissEl = row.querySelector('.bulk-remote-status-dismiss');
    if (statusEl) statusEl.textContent = prefixBulkStatusMessage(operationLabel, '진행 중…', startedAt);
    if (dismissEl) dismissEl.hidden = false;
    resultsBtn.addEventListener('click', function () {
      openBulkResultsModal(resultsStore);
    });
    dismissEl.addEventListener('click', function () {
      if (row.parentNode) row.parentNode.removeChild(row);
    });
    list.appendChild(row);
    return {
      statusEl: statusEl,
      dismissEl: dismissEl,
      resultsBtnEl: resultsBtn,
      resultsStore: resultsStore,
      operationLabel: operationLabel
    };
  }

  function formatBulkHostLabel(r) {
    var label = (r.hostname && String(r.hostname).trim()) || r.ip || '?';
    if (r.hostname && r.ip) {
      label = String(r.hostname).trim() + ' (' + r.ip + ')';
    }
    return label;
  }

  function formatConnectViaSuffix(r) {
    if (r.connect_ip && r.tried_ips && r.tried_ips.length > 1 && r.connect_ip !== r.tried_ips[0]) {
      return ' (' + r.connect_ip + ' 로 연결)';
    }
    return '';
  }

  function formatPushConfigResultLine(r) {
    var label = formatBulkHostLabel(r);
    if (r.status === 'success') {
      return label + ': 성공' + formatConnectViaSuffix(r);
    }
    return label + ': 실패 — ' + (r.message || '알 수 없음');
  }

  function formatRestartResultLine(r) {
    var label = formatBulkHostLabel(r);
    if (r.status === 'success' && r.verify_ok) {
      var detail = r.verify_detail ? ' — ' + r.verify_detail : '';
      return label + ': 재시작 확인됨' + formatConnectViaSuffix(r) + detail;
    }
    var msg = r.message || r.verify_detail || '알 수 없음';
    return label + ': 실패 — ' + msg;
  }

  function formatApplyUpdateResultLine(r) {
    var label = formatBulkHostLabel(r);
    if (r.status === 'skipped') {
      return label + ': 건너뜀 — ' + (r.message || '적용 불가');
    }
    if (r.status === 'success') {
      var ver = r.version ? ' (' + r.version + ')' : '';
      return label + ': 업데이트 적용 요청됨' + ver + formatConnectViaSuffix(r);
    }
    return label + ': 실패 — ' + (r.message || '알 수 없음');
  }

  function formatRollbackResultLine(r) {
    var label = formatBulkHostLabel(r);
    if (r.status === 'skipped') {
      return label + ': 건너뜀 — ' + (r.message || '롤백 불가');
    }
    if (r.status === 'success') {
      return label + ': 롤백 요청됨' + formatConnectViaSuffix(r);
    }
    return label + ': 실패 — ' + (r.message || '알 수 없음');
  }

  function formatBulkDoneSummary(evt, allOkText) {
    if (!evt || evt.total === 0) return '';
    if (evt.failed > 0 || (evt.skipped && evt.skipped > 0)) {
      var parts = [];
      if (evt.succeeded > 0) parts.push('성공 ' + evt.succeeded + '대');
      if (evt.failed > 0) parts.push('실패 ' + evt.failed + '대');
      if (evt.skipped > 0) parts.push('건너뜀 ' + evt.skipped + '대');
      return '완료: ' + parts.join(', ') + '.';
    }
    return formatBulkSummary(evt, allOkText);
  }

  function formatBulkResultsBody(results, formatLine) {
    if (!results || !results.length) return '(결과 없음)';
    return results.map(formatLine).join('\n');
  }

  function formatBulkSummary(evt, allOkText) {
    if (!evt || evt.total === 0) return '';
    if (evt.failed > 0) {
      return '완료: 성공 ' + evt.succeeded + '대, 실패 ' + evt.failed + '대.';
    }
    return evt.total + allOkText;
  }

  function openBulkResultsModal(store) {
    var modal = M.el('bulk-remote-results-modal');
    var title = M.el('bulk-remote-results-modal-title');
    var body = M.el('bulk-remote-results-body');
    var data = store || { lines: [], title: '' };
    if (title) {
      var titleText = data.title || '결과';
      if (data.finishedAt) titleText += ' — ' + formatBulkLogTimestamp(data.finishedAt);
      title.textContent = titleText;
    }
    if (body) body.textContent = (data.lines && data.lines.length) ? data.lines.join('\n') : '(결과 없음)';
    if (modal) {
      modal.hidden = false;
      modal.setAttribute('aria-hidden', 'false');
    }
    document.body.classList.add('config-modal-open');
  }

  function closeBulkResultsModal() {
    var modal = M.el('bulk-remote-results-modal');
    if (modal) {
      modal.hidden = true;
      modal.setAttribute('aria-hidden', 'true');
    }
    document.body.classList.remove('config-modal-open');
  }

  function initBulkResultsModal() {
    var closeBtn = M.el('bulk-remote-results-modal-close-btn');
    var dismissBtn = M.el('bulk-remote-results-modal-dismiss-btn');
    var backdrop = M.el('bulk-remote-results-modal') && M.el('bulk-remote-results-modal').querySelector('.config-editor-modal-backdrop');
    if (closeBtn) closeBtn.addEventListener('click', closeBulkResultsModal);
    if (dismissBtn) dismissBtn.addEventListener('click', closeBulkResultsModal);
    if (backdrop) backdrop.addEventListener('click', closeBulkResultsModal);
  }

  function refreshRemoteBulkButtonsState() {
    var domCount = countRemoteHostCards();
    var noHostsTitle = domCount === 0 ? 'Discovery로 원격 호스트를 먼저 찾으세요.' : '';
    ['push-config-all-remotes-btn', 'restart-all-remotes-btn'].forEach(function (id) {
      var bulkBtn = M.el(id);
      if (!bulkBtn || bulkBtn.getAttribute('data-busy') === '1') return;
      bulkBtn.disabled = domCount === 0;
      bulkBtn.title = noHostsTitle;
    });
    var rollbackAllBtn = M.el('rollback-all-remotes-btn');
    if (rollbackAllBtn && rollbackAllBtn.getAttribute('data-busy') !== '1') {
      rollbackAllBtn.disabled = !isBulkRollbackAllEnabled();
      rollbackAllBtn.title = getBulkRollbackAllDisabledTitle();
    }
    var applyAllBtn = M.el('apply-update-all-remotes-btn');
    if (applyAllBtn && applyAllBtn.getAttribute('data-busy') !== '1') {
      var applyCounts = getBulkApplyUpdateCounts();
      applyAllBtn.textContent = formatBulkApplyUpdateAllButtonLabel(applyCounts);
      applyAllBtn.disabled = !isBulkApplyUpdateAllEnabled();
      applyAllBtn.title = getBulkApplyUpdateAllDisabledTitle(applyCounts);
    }
  }

  /** Per-card staging vs remote version counts for bulk apply label and confirm. */
  function getBulkApplyUpdateCounts() {
    var total = countRemoteHostCards();
    var eligible = 0;
    var eligibleReachable = 0;
    var pending = false;
    var cards = document.querySelectorAll('#discovered-hosts .host-card:not(.self-card)');
    for (var i = 0; i < cards.length; i++) {
      var card = cards[i];
      var ip = card.getAttribute('data-host-ip') || '';
      if (!ip) continue;
      var st = M.remoteUpdateStatusByIP[ip];
      if (!st || st.pending) {
        pending = true;
        continue;
      }
      if (st.ok && st.can_apply && st.apply_version) {
        eligible++;
        if (M.isRemoteHostReachableForControl(card)) eligibleReachable++;
      }
    }
    return { total: total, eligible: eligible, eligibleReachable: eligibleReachable, pending: pending };
  }

  function formatBulkApplyUpdateAllButtonLabel(counts) {
    if (!counts || counts.total <= 0 || !getBulkApplyStagingVersion()) {
      return APPLY_UPDATE_ALL_LABEL;
    }
    if (counts.pending) {
      return APPLY_UPDATE_ALL_LABEL + ' (…/' + counts.total + ')';
    }
    return APPLY_UPDATE_ALL_LABEL + ' (' + counts.eligible + '/' + counts.total + ')';
  }

  /** Staging version key for bulk remote apply (newest staged; independent of local can_apply). */
  function getBulkApplyStagingVersion() {
    if (!M.lastUpdateStatus || !M.lastUpdateStatus.staging_versions || !M.lastUpdateStatus.staging_versions.length) {
      return '';
    }
    if (M.lastUpdateStatus.apply_version) return M.lastUpdateStatus.apply_version;
    return M.lastUpdateStatus.staging_versions[0];
  }

  function forEachReachableRemoteHostCard(fn) {
    var cards = document.querySelectorAll('#discovered-hosts .host-card:not(.self-card)');
    for (var i = 0; i < cards.length; i++) {
      var card = cards[i];
      if (!M.isRemoteHostReachableForControl(card)) continue;
      var ip = card.getAttribute('data-host-ip') || '';
      if (!ip) continue;
      fn(card, ip);
    }
  }

  /** Bulk apply: enable when ≥1 reachable remote can apply staged version (not all remotes). */
  function isBulkApplyUpdateAllEnabled() {
    var counts = getBulkApplyUpdateCounts();
    if (counts.total === 0) return false;
    if (!getBulkApplyStagingVersion()) return false;
    if (counts.pending) return false;
    return counts.eligibleReachable > 0;
  }

  function getBulkApplyUpdateAllDisabledTitle(counts) {
    counts = counts || getBulkApplyUpdateCounts();
    if (counts.total === 0) return 'Discovery로 원격 호스트를 먼저 찾으세요.';
    if (!getBulkApplyStagingVersion()) return '스테이징에 업로드된 버전이 없습니다.';
    if (counts.pending) return '원격 버전 확인 중…';
    if (counts.eligible === 0) {
      return '스테이징 버전과 비교해 업데이트 가능한 원격 호스트가 없습니다.';
    }
    if (counts.eligibleReachable === 0) {
      return '업데이트 가능 호스트가 있으나 헬스 실패·Discovery 미응답으로 실행할 수 없습니다.';
    }
    var version = getBulkApplyStagingVersion();
    return version ? ('스테이징 ' + version + ' — 적용 가능 ' + counts.eligible + '/' + counts.total + '대') : '';
  }

  function canRollbackFromVersionsList(versions) {
    if (!versions || !versions.length) return false;
    var currentVer = '';
    var previousVer = '';
    for (var i = 0; i < versions.length; i++) {
      var v = versions[i];
      if (v.is_current) currentVer = (v.version || '').trim();
      if (v.is_previous) previousVer = (v.version || '').trim();
    }
    if (!previousVer) return false;
    return currentVer !== previousVer;
  }

  function getBulkRollbackCounts() {
    var total = countRemoteHostCards();
    var eligible = 0;
    var eligibleReachable = 0;
    var pending = false;
    var cards = document.querySelectorAll('#discovered-hosts .host-card:not(.self-card)');
    for (var i = 0; i < cards.length; i++) {
      var card = cards[i];
      var ip = card.getAttribute('data-host-ip') || '';
      if (!ip) continue;
      var st = M.remoteRollbackStatusByIP[ip];
      if (!st || st.pending) {
        pending = true;
        continue;
      }
      if (st.ok && st.can_rollback) {
        eligible++;
        if (M.isRemoteHostReachableForControl(card)) eligibleReachable++;
      }
    }
    return { total: total, eligible: eligible, eligibleReachable: eligibleReachable, pending: pending };
  }

  function isBulkRollbackAllEnabled() {
    var counts = getBulkRollbackCounts();
    if (counts.total === 0) return false;
    if (counts.pending) return false;
    return counts.eligibleReachable > 0;
  }

  function getBulkRollbackAllDisabledTitle(counts) {
    counts = counts || getBulkRollbackCounts();
    if (counts.total === 0) return 'Discovery로 원격 호스트를 먼저 찾으세요.';
    if (counts.pending) return '원격 롤백 가능 여부 확인 중…';
    if (counts.eligible === 0) {
      return '롤백 가능한 원격이 없습니다 (current·previous 동일 또는 previous 없음).';
    }
    if (counts.eligibleReachable === 0) {
      return '롤백 가능 호스트가 있으나 헬스 실패·Discovery 미응답으로 실행할 수 없습니다.';
    }
    return '롤백 가능 ' + counts.eligible + '/' + counts.total + '대';
  }

  function runBulkHostsNDJSON(options) {
    var btn = options.buttonEl;
    if (!btn || btn.disabled || btn.getAttribute('data-busy') === '1') return;
    var operationLabel = options.operationLabel || options.label || '일괄 작업';
    var entry = createBulkRemoteStatusEntry(operationLabel, options.resultsTitle);
    if (!entry) return;
    var statusEl = entry.statusEl;
    var dismissEl = entry.dismissEl;
    var resultsBtn = entry.resultsBtnEl;
    var resultsStore = entry.resultsStore;
    var label = options.label;
    var apiPath = options.apiPath;
    var formatLine = options.formatLine;
    var formatSummary = options.formatSummary;
    var onDone = options.onDone || function () {};
    btn.setAttribute('data-busy', '1');
    btn.disabled = true;

    var hosts = collectRemoteHostsFromDOM();
    var requestBody = options.buildRequestBody
      ? options.buildRequestBody(hosts)
      : { hosts: hosts };
    fetch(M.API_BASE + apiPath, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(requestBody)
    })
      .then(function (res) {
        if (!res.ok || !res.body) {
          throw new Error('HTTP ' + res.status);
        }
        var reader = res.body.getReader();
        var decoder = new TextDecoder();
        var buffer = '';
        var doneHandled = false;
        var progressResults = [];

        function finish(evt) {
          if (doneHandled) return;
          doneHandled = true;
          var finishedAt = new Date();
          resultsStore.finishedAt = finishedAt;
          btn.textContent = label;
          btn.removeAttribute('data-busy');
          resultsStore.lines = progressResults.map(function (r) {
            return formatBulkResultLogLine(r.recordedAt || finishedAt, formatLine(r));
          });
          resultsStore.title = options.resultsTitle || '결과';
          if (resultsBtn) resultsBtn.hidden = progressResults.length === 0;
          if (statusEl) {
            var detail;
            if (evt && evt.total === 0) {
              detail = hosts.length
                ? '원격 호스트에 연결할 수 없습니다. Discovery를 다시 실행해 보세요.'
                : (options.emptyText || '대상 호스트가 없습니다. Discovery를 실행하세요.');
            } else if (evt) {
              detail = formatSummary(evt) || '요청이 중단되었습니다.';
            } else {
              detail = '요청이 중단되었습니다.';
            }
            statusEl.textContent = prefixBulkStatusMessage(operationLabel, detail, finishedAt);
          }
          if (dismissEl) dismissEl.hidden = false;
          refreshRemoteBulkButtonsState();
          onDone(evt, progressResults);
          M.fetchUpdateLog(true);
        }

        function processLine(line) {
          line = line.trim();
          if (!line) return;
          var evt;
          try {
            evt = JSON.parse(line);
          } catch (parseErr) {
            return;
          }
          if (evt.type === 'start') {
            btn.textContent = '0/' + evt.total;
          } else if (evt.type === 'progress') {
            btn.textContent = evt.current + '/' + evt.total;
            evt.recordedAt = new Date();
            progressResults.push(evt);
          } else if (evt.type === 'done') {
            finish(evt);
          }
        }

        function pump() {
          return reader.read().then(function (result) {
            if (result.done) {
              if (buffer.trim()) processLine(buffer);
              if (!doneHandled) finish(null);
              return;
            }
            buffer += decoder.decode(result.value, { stream: true });
            var lines = buffer.split('\n');
            buffer = lines.pop();
            for (var i = 0; i < lines.length; i++) processLine(lines[i]);
            return pump();
          });
        }
        return pump();
      })
      .catch(function () {
        btn.textContent = label;
        btn.removeAttribute('data-busy');
        if (statusEl) statusEl.textContent = prefixBulkStatusMessage(operationLabel, '요청 실패.', new Date());
        if (dismissEl) dismissEl.hidden = false;
        refreshRemoteBulkButtonsState();
      });
  }

  function refreshRemoteConfigEditorsAfterBulkPush() {
    var list = M.el('discovered-hosts');
    if (!list) return;
    var cards = list.querySelectorAll('.host-card:not(.self-card)');
    for (var i = 0; i < cards.length; i++) {
      var card = cards[i];
      var ip = card.getAttribute('data-host-ip');
      if (ip) M.fetchCurrentConfigForCard(card, ip);
    }
  }

  function runPushConfigToAllRemotes() {
    runBulkHostsNDJSON({
      buttonEl: M.el('push-config-all-remotes-btn'),
      operationLabel: PUSH_CONFIG_ALL_STATUS_LABEL,
      label: PUSH_CONFIG_ALL_LABEL,
      apiPath: '/current-config/push-local-all',
      formatLine: formatPushConfigResultLine,
      formatSummary: function (evt) { return formatBulkSummary(evt, '대 모두 복사했습니다.'); },
      resultsTitle: '설정 일괄 복사 결과',
      emptyText: '복사할 원격 호스트가 없습니다. Discovery를 실행하세요.',
      onDone: function () {
        refreshRemoteConfigEditorsAfterBulkPush();
        var list = M.el('discovered-hosts');
        if (!list) return;
        var cards = list.querySelectorAll('.host-card:not(.self-card)');
        for (var i = 0; i < cards.length; i++) {
          M.fetchServiceStatus(cards[i], cards[i].getAttribute('data-host-ip') || '');
        }
      }
    });
  }

  function runRestartAllRemotes() {
    runBulkHostsNDJSON({
      buttonEl: M.el('restart-all-remotes-btn'),
      operationLabel: RESTART_ALL_STATUS_LABEL,
      label: RESTART_ALL_LABEL,
      apiPath: '/service-control/restart-all',
      formatLine: formatRestartResultLine,
      formatSummary: function (evt) { return formatBulkSummary(evt, '대 모두 재시작되었습니다.'); },
      resultsTitle: '리모트 일괄 재시작 결과',
      emptyText: '재시작할 원격 호스트가 없습니다. Discovery를 실행하세요.',
      onDone: function () {
        var list = M.el('discovered-hosts');
        if (!list) return;
        var cards = list.querySelectorAll('.host-card:not(.self-card)');
        for (var i = 0; i < cards.length; i++) {
          var card = cards[i];
          var ip = card.getAttribute('data-host-ip') || '';
          if (ip) {
            M.fetchServiceStatus(card, ip);
            M.registerRemoteHealthMonitoring(card);
          }
        }
      }
    });
  }

  var bulkApplyUpdateConfirmProceed = null;

  function closeBulkApplyUpdateConfirmModal() {
    var modal = M.el('bulk-apply-update-confirm-modal');
    if (modal) {
      modal.hidden = true;
      modal.setAttribute('aria-hidden', 'true');
    }
    document.body.classList.remove('config-modal-open');
    bulkApplyUpdateConfirmProceed = null;
  }

  function openBulkApplyUpdateConfirmModal(message, onProceed) {
    var modal = M.el('bulk-apply-update-confirm-modal');
    var body = M.el('bulk-apply-update-confirm-modal-body');
    if (!modal || !body) {
      if (onProceed && window.confirm(message)) onProceed();
      return;
    }
    body.textContent = message;
    bulkApplyUpdateConfirmProceed = onProceed || null;
    modal.hidden = false;
    modal.setAttribute('aria-hidden', 'false');
    document.body.classList.add('config-modal-open');
    var proceedBtn = M.el('bulk-apply-update-confirm-proceed-btn');
    if (proceedBtn) proceedBtn.focus();
  }

  function initBulkApplyUpdateConfirmModal() {
    var modal = M.el('bulk-apply-update-confirm-modal');
    if (!modal || modal.getAttribute('data-bound')) return;
    modal.setAttribute('data-bound', '1');
    var backdrop = modal.querySelector('.config-editor-modal-backdrop');
    var closeBtn = M.el('bulk-apply-update-confirm-modal-close-btn');
    var cancelBtn = M.el('bulk-apply-update-confirm-cancel-btn');
    var proceedBtn = M.el('bulk-apply-update-confirm-proceed-btn');
    function cancel() {
      closeBulkApplyUpdateConfirmModal();
    }
    function proceed() {
      var fn = bulkApplyUpdateConfirmProceed;
      closeBulkApplyUpdateConfirmModal();
      if (fn) fn();
    }
    if (backdrop) backdrop.addEventListener('click', cancel);
    if (closeBtn) closeBtn.addEventListener('click', cancel);
    if (cancelBtn) cancelBtn.addEventListener('click', cancel);
    if (proceedBtn) proceedBtn.addEventListener('click', proceed);
  }

  function formatBulkApplyUpdateConfirmMessage(counts, bulkVersion) {
    var lines = [
      '리모트 ' + counts.total + '대 중 ' + counts.eligible + '대가 스테이징 버전 ' +
        bulkVersion + '(으)로 업데이트 가능합니다.',
      '',
      '환경설정: 오른쪽 「업데이트」 패널의 「이전버전의 환경설정 파일 재사용」 설정을 모든 원격에 동일하게 적용합니다.',
      '(리모트 카드별 체크박스는 따르지 않습니다.)'
    ];
    if (M.getReusePreviousConfig()) {
      lines.push('· 체크됨 — 각 원격 호스트의 current config(agent.local.yml)를 유지합니다.');
    } else if (M.isLocalReuseConfigVisible()) {
      lines.push('· 체크 해제 — 스테이징 번들에 포함된 config를 각 원격에 적용합니다.');
    } else {
      lines.push('· 스테이징 번들에 포함된 config를 각 원격에 적용합니다.');
    }
    lines.push('', '계속하시겠습니까?');
    return lines.join('\n');
  }

  function runApplyUpdateAllRemotes() {
    if (!isBulkApplyUpdateAllEnabled()) return;
    var counts = getBulkApplyUpdateCounts();
    var bulkVersion = getBulkApplyStagingVersion();
    openBulkApplyUpdateConfirmModal(formatBulkApplyUpdateConfirmMessage(counts, bulkVersion), function () {
      runBulkHostsNDJSON({
        buttonEl: M.el('apply-update-all-remotes-btn'),
        operationLabel: APPLY_UPDATE_ALL_STATUS_LABEL,
        label: formatBulkApplyUpdateAllButtonLabel(getBulkApplyUpdateCounts()),
        apiPath: '/apply-update-all',
        buildRequestBody: function (hosts) {
          return {
            hosts: hosts,
            version: bulkVersion,
            agent_variant: M.getSelectedAgentVariant(),
            reuse_previous_config: M.getReusePreviousConfig()
          };
        },
        formatLine: formatApplyUpdateResultLine,
        formatSummary: function (evt) {
          return formatBulkDoneSummary(evt, '대 모두에 업데이트를 적용했습니다.');
        },
        resultsTitle: '리모트 일괄 업데이트 적용 결과',
        emptyText: '적용할 원격 호스트가 없습니다. Discovery를 실행하세요.',
        onDone: function (evt, progressResults) {
          refreshRemoteCardsAfterBulkApplyUpdate(progressResults, bulkVersion);
          M.fetchUpdateStatus();
          var list = M.el('discovered-hosts');
          if (!list) return;
          var cards = list.querySelectorAll('.host-card:not(.self-card)');
          for (var i = 0; i < cards.length; i++) {
            var card = cards[i];
            var ip = card.getAttribute('data-host-ip') || '';
            if (ip) M.registerRemoteHealthMonitoring(card);
          }
        }
      });
    });
  }

  function runRollbackAllRemotes() {
    if (!isBulkRollbackAllEnabled()) return;
    runBulkHostsNDJSON({
      buttonEl: M.el('rollback-all-remotes-btn'),
      operationLabel: ROLLBACK_ALL_STATUS_LABEL,
      label: ROLLBACK_ALL_LABEL,
      apiPath: '/versions/rollback-all',
      formatLine: formatRollbackResultLine,
      formatSummary: function (evt) {
        return formatBulkDoneSummary(evt, '대 모두 롤백했습니다.');
      },
      resultsTitle: '리모트 일괄 롤백 결과',
      emptyText: '롤백할 원격 호스트가 없습니다. Discovery를 실행하세요.',
      onDone: function () {
        M.fetchUpdateStatus();
        M.fetchRollbackStatusForAllRemoteHosts();
        var list = M.el('discovered-hosts');
        if (!list) return;
        var cards = list.querySelectorAll('.host-card:not(.self-card)');
        for (var i = 0; i < cards.length; i++) {
          var card = cards[i];
          var ip = card.getAttribute('data-host-ip') || '';
          if (ip) {
            M.fetchServiceStatus(card, ip);
            M.fetchUpdateLogForCard(card, ip);
            M.fetchVersionsListForCard(card, ip);
            M.registerRemoteHealthMonitoring(card);
          }
        }
      }
    });
  }


  // exports
  M.canRollbackFromVersionsList = canRollbackFromVersionsList;
  M.closeBulkApplyUpdateConfirmModal = closeBulkApplyUpdateConfirmModal;
  M.closeBulkResultsModal = closeBulkResultsModal;
  M.collectRemoteHostsFromDOM = collectRemoteHostsFromDOM;
  M.countRemoteHostCards = countRemoteHostCards;
  M.createBulkRemoteStatusEntry = createBulkRemoteStatusEntry;
  M.forEachReachableRemoteHostCard = forEachReachableRemoteHostCard;
  M.formatApplyUpdateResultLine = formatApplyUpdateResultLine;
  M.formatBulkApplyUpdateAllButtonLabel = formatBulkApplyUpdateAllButtonLabel;
  M.formatBulkApplyUpdateConfirmMessage = formatBulkApplyUpdateConfirmMessage;
  M.formatBulkDoneSummary = formatBulkDoneSummary;
  M.formatBulkHostLabel = formatBulkHostLabel;
  M.formatBulkLogTimestamp = formatBulkLogTimestamp;
  M.formatBulkResultLogLine = formatBulkResultLogLine;
  M.formatBulkResultsBody = formatBulkResultsBody;
  M.formatBulkSummary = formatBulkSummary;
  M.formatConnectViaSuffix = formatConnectViaSuffix;
  M.formatPushConfigResultLine = formatPushConfigResultLine;
  M.formatRestartResultLine = formatRestartResultLine;
  M.formatRollbackResultLine = formatRollbackResultLine;
  M.getBulkApplyStagingVersion = getBulkApplyStagingVersion;
  M.getBulkApplyUpdateAllDisabledTitle = getBulkApplyUpdateAllDisabledTitle;
  M.getBulkApplyUpdateCounts = getBulkApplyUpdateCounts;
  M.getBulkRollbackAllDisabledTitle = getBulkRollbackAllDisabledTitle;
  M.getBulkRollbackCounts = getBulkRollbackCounts;
  M.initBulkApplyUpdateConfirmModal = initBulkApplyUpdateConfirmModal;
  M.initBulkResultsModal = initBulkResultsModal;
  M.isBulkApplyUpdateAllEnabled = isBulkApplyUpdateAllEnabled;
  M.isBulkRollbackAllEnabled = isBulkRollbackAllEnabled;
  M.mergeHostIpsFromResponseIntoCard = mergeHostIpsFromResponseIntoCard;
  M.mergeHostIpsIntoCard = mergeHostIpsIntoCard;
  M.mergeRespondedFromIntoCard = mergeRespondedFromIntoCard;
  M.openBulkApplyUpdateConfirmModal = openBulkApplyUpdateConfirmModal;
  M.openBulkResultsModal = openBulkResultsModal;
  M.prefixBulkStatusMessage = prefixBulkStatusMessage;
  M.refreshRemoteBulkButtonsState = refreshRemoteBulkButtonsState;
  M.refreshRemoteCardsAfterBulkApplyUpdate = refreshRemoteCardsAfterBulkApplyUpdate;
  M.refreshRemoteConfigEditorsAfterBulkPush = refreshRemoteConfigEditorsAfterBulkPush;
  M.runApplyUpdateAllRemotes = runApplyUpdateAllRemotes;
  M.runBulkHostsNDJSON = runBulkHostsNDJSON;
  M.runPushConfigToAllRemotes = runPushConfigToAllRemotes;
  M.runRestartAllRemotes = runRestartAllRemotes;
  M.runRollbackAllRemotes = runRollbackAllRemotes;

})(window.MolMaintenance = window.MolMaintenance || {});
