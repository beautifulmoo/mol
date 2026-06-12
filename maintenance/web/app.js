(function () {
  var _api = (typeof window !== 'undefined' && window.__CONTRABASS_API_PREFIX__) || '/api/v1';
  if (typeof _api === 'string' && _api.length > 1 && _api.charAt(_api.length - 1) === '/') {
    _api = _api.slice(0, -1);
  }
  var API_BASE = _api;

  /** Prefer installed build variant (control|compute); unknown → compute. */
  function defaultAgentVariantFromBuild(buildVariant) {
    var v = String(buildVariant || '').toLowerCase().trim();
    if (v === 'control' || v === 'compute') return v;
    return 'compute';
  }

  function setVariantRadioSelection(scopeEl, variant) {
    if (!scopeEl) return;
    var v = defaultAgentVariantFromBuild(variant);
    var radios = scopeEl.querySelectorAll('input[type="radio"][value="compute"], input[type="radio"][value="control"]');
    for (var i = 0; i < radios.length; i++) {
      radios[i].checked = radios[i].value === v;
    }
  }

  function cardVariantRadiosHtml(hostIp, preferredVariant) {
    var pref = defaultAgentVariantFromBuild(preferredVariant);
    var name = 'card-agent-variant-' + (hostIp || '');
    return '<span class="card-variant-selector" hidden>' +
      '<span class="card-variant-label">적용 바이너리</span>' +
      '<label class="card-variant-option"><input type="radio" name="' + name + '" value="compute"' +
      (pref === 'compute' ? ' checked' : '') + '> <code>compute</code></label>' +
      '<label class="card-variant-option"><input type="radio" name="' + name + '" value="control"' +
      (pref === 'control' ? ' checked' : '') + '> <code>control</code></label>' +
      '</span>';
  }

  function cardReuseConfigHtml() {
    return '<label class="reuse-config-option card-reuse-config-option" hidden>' +
      '<input type="checkbox" class="card-reuse-previous-config" checked>' +
      '<span>이전버전의 환경설정 파일 재사용</span></label>';
  }

  function hasStagingOnServer() {
    return !!(lastUpdateStatus.staging_versions && lastUpdateStatus.staging_versions.length > 0);
  }

  function updateReuseConfigVisibility() {
    var show = hasStagingOnServer();
    var localWrap = el('reuse-previous-config-wrap');
    if (localWrap) localWrap.hidden = !show;
    var cardOpts = document.querySelectorAll('.card-reuse-config-option');
    for (var i = 0; i < cardOpts.length; i++) {
      cardOpts[i].hidden = !show;
    }
  }

  function applyLocalVariantDefault() {
    var fs = el('agent-variant-fieldset');
    if (!fs || fs.hidden) return;
    var selfCard = document.querySelector('#self-info .host-card.self-card') ||
      document.querySelector('.host-card.self-card');
    setVariantRadioSelection(fs, selfCard ? selfCard.getAttribute('data-build-variant') : '');
  }

  function getSelectedAgentVariant() {
    var sel = document.querySelector('input[name="agent-variant"]:checked');
    return (sel && sel.value) ? sel.value : 'compute';
  }

  function getReusePreviousConfig() {
    var wrap = el('reuse-previous-config-wrap');
    if (!wrap || wrap.hidden) return false;
    var cb = el('reuse-previous-config');
    return !(cb && !cb.checked);
  }

  function getCardReusePreviousConfig(cardEl) {
    if (!cardEl) return false;
    var wrap = cardEl.querySelector('.card-reuse-config-option');
    if (!wrap || wrap.hidden) return false;
    var cb = wrap.querySelector('.card-reuse-previous-config');
    return !(cb && !cb.checked);
  }

  function isLocalReuseConfigVisible() {
    var wrap = el('reuse-previous-config-wrap');
    return !!(wrap && !wrap.hidden);
  }

  function isCardReuseConfigVisible(cardEl) {
    if (!cardEl) return false;
    var wrap = cardEl.querySelector('.card-reuse-config-option');
    return !!(wrap && !wrap.hidden);
  }

  var REUSE_CONFIG_CONFIRM_MSG =
    '정말 이전 버전의 환경설정 파일을 사용하지 않고 번들에 포함된 환경설정 파일을 사용하시겠습니까?';
  var REUSE_CONFIG_DECLINE_MSG =
    '「이전버전의 환경설정 파일 재사용」 체크박스를 선택한 후 다시 「업데이트 적용」 버튼을 눌러 주세요.';

  function confirmApplyConfigChoice(reusePreviousConfig, reuseCheckboxVisible, onProceed) {
    if (reuseCheckboxVisible && !reusePreviousConfig) {
      if (window.confirm(REUSE_CONFIG_CONFIRM_MSG)) {
        onProceed();
      } else {
        window.alert(REUSE_CONFIG_DECLINE_MSG);
      }
      return;
    }
    onProceed();
  }

  function getCardAgentVariant(cardEl) {
    if (!cardEl) return 'compute';
    var sel = cardEl.querySelector('.card-variant-selector input[type="radio"]:checked');
    if (sel && sel.value) return sel.value;
    return defaultAgentVariantFromBuild(cardEl.getAttribute('data-build-variant'));
  }

  var remoteHealthState = {};

  function getRemoteHealthCfg() {
    var h = typeof window !== 'undefined' && window.__CONTRABASS_REMOTE_HEALTH__;
    if (h && typeof h.intervalSec === 'number') {
      return {
        intervalSec: h.intervalSec,
        timeoutSec: h.timeoutSec,
        failureThreshold: h.failureThreshold,
        jitterSec: h.jitterSec
      };
    }
    return { intervalSec: 10, timeoutSec: 2, failureThreshold: 3, jitterSec: 2 };
  }

  function setRemoteHealthCardUI(card, dead, message) {
    if (!card) return;
    var banner = card.querySelector('.remote-health-banner');
    var btn = card.querySelector('.remote-health-recheck-btn');
    if (banner) {
      banner.hidden = !dead;
      banner.textContent = dead ? (message || 'HTTP 헬스체크 실패') : '';
    }
    if (btn) btn.hidden = !dead;
    var row = card.closest && card.closest('.host-row');
    var wasDead = !!(row && row.classList.contains('host-row--remote-health-dead'));
    if (row) row.classList.toggle('host-row--remote-health-dead', !!dead);
    if (card.classList.contains('self-card')) return;
    if (wasDead !== !!dead) {
      updateAllHostApplyButtons();
    }
  }

  function scheduleRemoteHealthTick(ip) {
    var st = remoteHealthState[ip];
    if (!st) return;
    if (st.timerId != null) {
      clearTimeout(st.timerId);
      st.timerId = null;
    }
    if (document.hidden) return;
    var cfg = getRemoteHealthCfg();
    var delayMs = cfg.intervalSec * 1000 + Math.random() * cfg.jitterSec * 1000;
    st.timerId = setTimeout(function () {
      st.timerId = null;
      execRemoteHealthCheck(ip, false);
    }, delayMs);
  }

  function refreshRemoteHostAfterHealthOk(card, ip) {
    if (!card || !ip) return;
    fetch(API_BASE + '/host-info?ip=' + encodeURIComponent(ip))
      .then(function (res) { return res.json(); })
      .then(function (body) {
        if (body.status === 'success' && body.data) {
          updateHostCardDetails(card, body.data);
          mergeHostIpsFromResponseIntoCard(card, body.data);
          if (body.data.responded_from_ip) mergeRespondedFromIntoCard(card, body.data.responded_from_ip);
          var row = card.closest && card.closest('.host-row');
          if (row) updateHostRowLabel(row, body.data, false);
          fetchUpdateLogForCard(card, ip);
          fetchCurrentConfigForCard(card, ip);
          fetchVersionsListForCard(card, ip);
          fetchServiceStatus(card, ip);
          fetchUpdateStatus();
          updateAllHostApplyButtons();
        }
      })
      .catch(function () {});
  }

  function onRemoteHealthTransportFail(ip, card, detail) {
    var st = remoteHealthState[ip];
    if (!st) return;
    st.failures += 1;
    var cfg = getRemoteHealthCfg();
    if (st.failures >= cfg.failureThreshold) {
      st.dead = true;
      setRemoteHealthCardUI(
        card,
        true,
        'HTTP 헬스체크가 ' + cfg.failureThreshold + '회 연속 실패했습니다. 원격 API(' + (detail || '응답 없음') + ')에 연결할 수 없습니다.'
      );
    }
  }

  function execRemoteHealthCheck(ip, manual) {
    var list = el('discovered-hosts');
    var card = list ? findHostCardByIp(list, ip) : null;
    if (!card) {
      delete remoteHealthState[ip];
      return;
    }
    var btn = card.querySelector('.remote-health-recheck-btn');
    if (manual && btn) btn.disabled = true;
    fetch(API_BASE + '/remote-health-check?ip=' + encodeURIComponent(ip))
      .then(function (res) { return res.json(); })
      .then(function (body) {
        var st = remoteHealthState[ip];
        if (body.status === 'success') {
          if (st) {
            st.failures = 0;
            st.dead = false;
          }
          setRemoteHealthCardUI(card, false, '');
          if (manual) {
            refreshRemoteHostAfterHealthOk(card, ip);
          }
        } else {
          onRemoteHealthTransportFail(ip, card, typeof body.data === 'string' ? body.data : '');
        }
      })
      .catch(function () {
        onRemoteHealthTransportFail(ip, card, '요청 실패');
      })
      .finally(function () {
        if (manual && btn) btn.disabled = false;
        if (!document.hidden) {
          scheduleRemoteHealthTick(ip);
        }
      });
  }

  function ensureRemoteHealthForIp(ip) {
    if (!ip) return;
    if (!remoteHealthState[ip]) {
      remoteHealthState[ip] = { failures: 0, timerId: null, dead: false };
      scheduleRemoteHealthTick(ip);
    }
  }

  function registerRemoteHealthMonitoring(card) {
    if (!card || card.classList.contains('self-card')) return;
    var ip = card.getAttribute('data-host-ip');
    if (!ip) return;
    ensureRemoteHealthForIp(ip);
  }

  function bindRemoteHealthForCard(cardEl) {
    if (!cardEl || cardEl.classList.contains('self-card')) return;
    var ip = cardEl.getAttribute('data-host-ip') || '';
    var recheckBtn = cardEl.querySelector('.remote-health-recheck-btn');
    if (recheckBtn) {
      recheckBtn.addEventListener('click', function () {
        if (!ip) return;
        execRemoteHealthCheck(ip, true);
      });
    }
    registerRemoteHealthMonitoring(cardEl);
  }

  function enumerateDiscoveredRemoteHealth() {
    var list = el('discovered-hosts');
    if (!list) return;
    var cards = list.querySelectorAll('.host-card');
    for (var i = 0; i < cards.length; i++) {
      registerRemoteHealthMonitoring(cards[i]);
    }
  }

  const serverIconSvg = '<svg class="host-icon" xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="currentColor" aria-hidden="true"><rect x="2" y="4" width="20" height="4" rx="1"/><rect x="2" y="10" width="20" height="4" rx="1"/><rect x="2" y="16" width="20" height="4" rx="1"/><circle cx="6" cy="6" r="0.8"/><circle cx="6" cy="12" r="0.8"/><circle cx="6" cy="18" r="0.8"/></svg>';

  function el(id) {
    return document.getElementById(id);
  }

  function getHostRowLabel(host, isSelf) {
    if (isSelf) {
      var name = (host.hostname && host.hostname.trim()) ? host.hostname.trim() : '로컬';
      var sub = host.responded_from_ip || host.host_ip || (host.host_ips && host.host_ips[0]) || '-';
      return name + ' · ' + sub;
    }
    var name = (host.hostname && host.hostname.trim()) ? host.hostname.trim() : (host.host_ip || '호스트');
    var sub = host.host_ip || ((host.cpu_uuid || '').trim().slice(0, 8)) || '-';
    return name + ' · ' + sub;
  }

  var REMOTE_HOST_TINT_COUNT = 10;
  var discoveryCountdownTimerId = null;
  var discoverySecondsLeft = 0;
  var discoveryHostCount = 0;

  function getDiscoveryTimeoutSec() {
    var d = typeof window !== 'undefined' && window.__CONTRABASS_DISCOVERY__;
    if (d && typeof d.timeoutSec === 'number' && d.timeoutSec > 0) {
      return d.timeoutSec;
    }
    return 10;
  }

  function stopDiscoveryCountdown() {
    if (discoveryCountdownTimerId != null) {
      clearInterval(discoveryCountdownTimerId);
      discoveryCountdownTimerId = null;
    }
  }

  function formatDiscoveryProgressStatus(secondsLeft, hostCount) {
    return 'Discovery 진행 중… ' + secondsLeft + '초 (호스트 ' + hostCount + '개, 응답 오는 대로 갱신)';
  }

  function startDiscoveryCountdown(statusEl, hostCount) {
    stopDiscoveryCountdown();
    discoveryHostCount = hostCount;
    discoverySecondsLeft = getDiscoveryTimeoutSec();
    if (statusEl) statusEl.textContent = formatDiscoveryProgressStatus(discoverySecondsLeft, discoveryHostCount);
    discoveryCountdownTimerId = setInterval(function () {
      discoverySecondsLeft -= 1;
      if (discoverySecondsLeft < 0) discoverySecondsLeft = 0;
      if (statusEl) statusEl.textContent = formatDiscoveryProgressStatus(discoverySecondsLeft, discoveryHostCount);
      if (discoverySecondsLeft <= 0) stopDiscoveryCountdown();
    }, 1000);
  }

  function refreshDiscoveryProgressStatus(statusEl, hostCount) {
    discoveryHostCount = hostCount;
    if (!statusEl || discoveryCountdownTimerId == null) return;
    statusEl.textContent = formatDiscoveryProgressStatus(discoverySecondsLeft, discoveryHostCount);
  }

  function refreshRemoteHostRowTints() {
    var list = el('discovered-hosts');
    if (!list) return;
    var rows = list.querySelectorAll('.host-row:not(.host-row--local)');
    for (var i = 0; i < rows.length; i++) {
      var row = rows[i];
      for (var t = 0; t < REMOTE_HOST_TINT_COUNT; t++) {
        row.classList.remove('host-row--tint-' + t);
      }
      row.classList.add('host-row--tint-' + (i % REMOTE_HOST_TINT_COUNT));
    }
  }

  function discoveryTrackKeyFromHost(host) {
    if (!host) return '';
    var uuid = String(host.cpu_uuid || '').trim();
    if (uuid) return 'uuid:' + uuid.toLowerCase();
    var ip = String(host.host_ip || '').trim();
    if (ip) return 'ip:' + ip;
    return '';
  }

  function discoveryTrackKeyFromCard(card) {
    if (!card) return '';
    var uuid = (card.getAttribute('data-cpu-uuid') || '').trim();
    if (uuid) return 'uuid:' + uuid.toLowerCase();
    var ip = (card.getAttribute('data-host-ip') || '').trim();
    if (ip) return 'ip:' + ip;
    return '';
  }

  function isRemoteHostReachableForControl(card) {
    if (!card || card.classList.contains('self-card')) return true;
    var row = card.closest && card.closest('.host-row');
    if (!row || row.classList.contains('host-row--local')) return true;
    if (row.classList.contains('host-row--remote-health-dead')) return false;
    if (row.classList.contains('host-row--discovery-missed')) return false;
    return true;
  }

  function remoteHostUnreachableReason(card) {
    if (!card || card.classList.contains('self-card')) return '';
    var row = card.closest && card.closest('.host-row');
    if (!row || row.classList.contains('host-row--local')) return '';
    if (row.classList.contains('host-row--remote-health-dead')) {
      return 'HTTP 헬스체크 실패 상태에서는 사용할 수 없습니다.';
    }
    if (row.classList.contains('host-row--discovery-missed')) {
      return '이번 Discovery에서 응답하지 않은 호스트에서는 사용할 수 없습니다.';
    }
    return '';
  }

  function updateRemoteServiceControlButtons(card) {
    if (!card || card.classList.contains('self-card')) return;
    var reachable = isRemoteHostReachableForControl(card);
    var reason = remoteHostUnreachableReason(card);
    var restartBtn = card.querySelector('.service-restart-btn');
    if (restartBtn) {
      restartBtn.disabled = !reachable;
      restartBtn.title = reachable ? '' : reason;
    }
  }

  function updateAllRemoteServiceControlButtons() {
    var cards = document.querySelectorAll('.host-card:not(.self-card)');
    for (var i = 0; i < cards.length; i++) {
      updateRemoteServiceControlButtons(cards[i]);
    }
  }

  function setDiscoveryMissUI(row, missed) {
    if (!row || row.classList.contains('host-row--local')) return;
    row.classList.toggle('host-row--discovery-missed', !!missed);
    var badge = row.querySelector('.host-row__discovery-miss-badge');
    if (badge) badge.hidden = !missed;
    var card = row.querySelector('.host-card');
    if (card) {
      var banner = card.querySelector('.card-discovery-miss-banner');
      if (banner) banner.hidden = !missed;
      updateRemoteServiceControlButtons(card);
    }
  }

  function clearAllDiscoveryMissMarks(list) {
    if (!list) return;
    var rows = list.querySelectorAll('.host-row:not(.host-row--local)');
    for (var i = 0; i < rows.length; i++) {
      setDiscoveryMissUI(rows[i], false);
    }
  }

  function finalizeDiscoveryMissMarks(list, respondedKeys) {
    if (!list) return 0;
    clearAllDiscoveryMissMarks(list);
    var missed = 0;
    var rows = list.querySelectorAll('.host-row:not(.host-row--local)');
    for (var i = 0; i < rows.length; i++) {
      var row = rows[i];
      var card = row.querySelector('.host-card');
      var key = discoveryTrackKeyFromCard(card);
      var didRespond = key && respondedKeys[key];
      setDiscoveryMissUI(row, !!key && !didRespond);
      if (key && !didRespond) missed++;
    }
    return missed;
  }

  function formatDiscoveryDoneStatus(totalCards, respondedCount, missedCount) {
    if (!totalCards) {
      return 'Discovery 완료 (결과 없음).';
    }
    var parts = ['호스트 ' + totalCards + '개 표시'];
    parts.push('이번 Discovery 응답 ' + respondedCount + '대');
    if (missedCount > 0) {
      parts.push('미응답 ' + missedCount + '대');
    }
    return parts.join(' · ') + '.';
  }

  function renderHostRow(host, isSelf) {
    var row = document.createElement('div');
    row.className = 'host-row' + (isSelf ? ' host-row--local' : '');
    if (host.host_ip) row.setAttribute('data-host-ip', host.host_ip);
    var label = getHostRowLabel(host, isSelf);
    var card = renderHostCard(host, isSelf);
    row.innerHTML =
      '<div class="host-row__header" role="button" tabindex="0" aria-expanded="false">' +
      '<span class="host-row__dot" aria-hidden="true"></span>' +
      '<span class="host-row__label">' + escapeHtml(label) + '</span>' +
      (isSelf ? '' : '<span class="host-row__discovery-miss-badge" hidden>이번 Discovery 미응답</span>') +
      '<span class="host-row__expand-icon" aria-hidden="true">▶</span>' +
      '</div>' +
      '<div class="host-row__body"></div>';
    row.querySelector('.host-row__body').appendChild(card);
    row.setAttribute('data-hostname', host.hostname || '');
    row.setAttribute('data-cpu-uuid', host.cpu_uuid || '');
    bindHostRowToggle(row);
    return row;
  }

  function bindHostRowToggle(row) {
    var header = row && row.querySelector('.host-row__header');
    if (!header) return;
    function toggle() {
      var expanded = row.classList.toggle('host-row--expanded');
      header.setAttribute('aria-expanded', expanded);
        if (expanded) {
        var card = row.querySelector('.host-card');
        if (card && !card.classList.contains('self-card')) {
          var ip = card.getAttribute('data-host-ip');
          if (ip) {
            fetchUpdateLogForCard(card, ip);
            fetchCurrentConfigForCard(card, ip);
            fetchVersionsListForCard(card, ip);
          }
        }
      }
    }
    header.addEventListener('click', toggle);
    header.addEventListener('keydown', function (e) {
      if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); toggle(); }
    });
  }

  function updateHostRowLabel(row, host, isSelf) {
    if (!row) return;
    var labelEl = row.querySelector('.host-row__label');
    if (labelEl) labelEl.textContent = getHostRowLabel(host || {}, isSelf);
  }

  function updateHostRowDot(row, isRunning) {
    if (!row) return;
    var dot = row.querySelector('.host-row__dot');
    if (!dot) return;
    dot.classList.remove('host-row__dot--running', 'host-row__dot--stopped');
    if (isRunning) dot.classList.add('host-row__dot--running');
    else dot.classList.add('host-row__dot--stopped');
  }

  function renderHostCard(host, isSelf) {
    const div = document.createElement('div');
    div.className = 'host-card' + (isSelf ? ' self-card' : '');
    if (host.host_ip) {
      div.setAttribute('data-host-ip', host.host_ip);
    }
    div.setAttribute('data-host-version', host.version || '');
    div.setAttribute('data-build-variant', defaultAgentVariantFromBuild(host.build_variant));
    var serviceActionsHtml =
      '<button type="button" class="service-btn status-refresh-btn">상태 새로고침</button>' +
      '<button type="button" class="service-btn service-restart-btn">서비스 재시작</button>';
    var remoteUpdateBarHtml = isSelf ? '' :
      '<div class="card-remote-update-bar">' +
      '<div class="card-update-apply-options">' +
      cardReuseConfigHtml() +
      cardVariantRadiosHtml(host.host_ip || '', host.build_variant) +
      '</div>' +
      '<div class="card-update-apply-actions">' +
      '<button type="button" class="service-btn service-start apply-update-host" disabled>업데이트 적용</button>' +
      '</div></div>';
    var statusRowHtml = '<div class="self-status-row">' +
      '<div class="service-status-block">' +
      '<div class="service-status-header-row">' +
      '<div class="service-status-header" role="button" tabindex="0" aria-expanded="false">' +
      '<span class="service-status-icon" aria-hidden="true">▶</span> ' +
      '<span class="service-status-summary">불러오는 중…</span>' +
      '</div>' +
      '<div class="service-status-actions">' + serviceActionsHtml + '</div>' +
      '</div>' +
      '<pre class="service-status-output"></pre>' +
      '</div>' +
      remoteUpdateBarHtml +
      '</div>';
    var rightColumnSelf = '<div class="self-card-right-column">' +
      '<div class="card-right-log">' +
      '<h4 class="card-right-title">업데이트 기록 (최근 10건)</h4>' +
      '<div id="self-update-rollback-warning" class="update-rollback-warning" role="alert" aria-live="polite" hidden></div>' +
      '<button type="button" id="self-update-log-refresh-btn" class="service-btn">로그 새로고침</button>' +
      '<pre id="self-update-log-output" class="update-log-output card-right-log-output">(새로고침으로 로그 불러오기)</pre>' +
      '</div>' +
      '<div class="card-right-config">' +
      '<h4 class="card-right-title">agent.local.yml (current)</h4>' +
      '<textarea id="self-current-config-editor" class="config-editor card-right-config-editor" placeholder="불러오기로 current 버전의 agent.local.yml을 불러옵니다." spellcheck="false"></textarea>' +
      '<div class="card-right-config-actions">' +
      '<button type="button" id="self-current-config-load-btn" class="service-btn">불러오기</button>' +
      '<button type="button" id="self-current-config-save-btn" class="service-btn">저장</button>' +
      '<button type="button" id="self-current-config-expand-btn" class="service-btn">펼쳐보기</button>' +
      '<span id="self-current-config-status" class="discovery-status" aria-live="polite"></span>' +
      '</div></div>' +
      '<div class="card-right-versions">' +
      '<h4 class="card-right-title">설치된 버전 (versions)</h4>' +
      '<p class="card-versions-desc update-desc">current·previous는 삭제할 수 없습니다.</p>' +
      '<button type="button" id="self-versions-list-refresh-btn" class="service-btn">목록 새로고침</button>' +
      '<div id="self-versions-list-container" class="versions-list-container"><div class="versions-loading">불러오는 중…</div></div>' +
      '<div class="versions-actions">' +
      '<button type="button" id="self-versions-remove-btn" class="service-btn" disabled title="선택한 버전을 versions에서 삭제합니다">선택한 버전 삭제</button>' +
      '<span id="self-versions-status" class="discovery-status" aria-live="polite"></span>' +
      '</div>' +
      '<div class="versions-switch-row">' +
      '<label for="self-versions-switch-select">이 버전으로 서비스</label> ' +
      '<select id="self-versions-switch-select">' +
      '<option value="">버전 선택…</option></select> ' +
      '<button type="button" id="self-versions-switch-btn" class="service-btn" disabled title="선택한 버전으로 서비스합니다 (update.sh)">이 버전으로 적용</button> ' +
      '<span id="self-versions-switch-hint" class="versions-switch-hint" aria-live="polite"></span>' +
      '</div></div>' +
      '</div>';
    var rightColumnRemote = '<div class="self-card-right-column">' +
      '<div class="card-right-log">' +
      '<h4 class="card-right-title">업데이트 기록 (최근 10건)</h4>' +
      '<div class="update-rollback-warning card-update-rollback-warning" role="alert" aria-live="polite" hidden></div>' +
      '<button type="button" class="service-btn card-update-log-refresh-btn">로그 새로고침</button>' +
      '<pre class="update-log-output card-right-log-output">(새로고침으로 로그 불러오기)</pre>' +
      '</div>' +
      '<div class="card-right-config">' +
      '<h4 class="card-right-title">agent.local.yml (current)</h4>' +
      '<textarea class="config-editor card-right-config-editor" placeholder="불러오기로 current 버전의 agent.local.yml을 불러옵니다." spellcheck="false"></textarea>' +
      '<div class="card-right-config-actions">' +
      '<button type="button" class="service-btn card-current-config-load-btn">불러오기</button>' +
      '<button type="button" class="service-btn card-current-config-save-btn">저장</button>' +
      '<button type="button" class="service-btn card-current-config-expand-btn">펼쳐보기</button>' +
      '<span class="discovery-status card-current-config-status" aria-live="polite"></span>' +
      '</div></div>' +
      '<div class="card-right-versions">' +
      '<h4 class="card-right-title">설치된 버전 (versions)</h4>' +
      '<p class="card-versions-desc update-desc">current·previous는 삭제할 수 없습니다.</p>' +
      '<button type="button" class="service-btn card-versions-list-refresh-btn">목록 새로고침</button>' +
      '<div class="versions-list-container card-right-versions-list-container"><div class="versions-loading">불러오는 중…</div></div>' +
      '<div class="versions-actions">' +
      '<button type="button" class="service-btn card-versions-remove-btn" disabled title="선택한 버전을 versions에서 삭제합니다">선택한 버전 삭제</button>' +
      '<span class="discovery-status card-versions-status" aria-live="polite"></span>' +
      '</div>' +
      '<div class="versions-switch-row">' +
      '<label class="card-versions-switch-label">이 버전으로 서비스</label> ' +
      '<select class="card-versions-switch-select">' +
      '<option value="">버전 선택…</option></select> ' +
      '<button type="button" class="service-btn card-versions-switch-btn" disabled title="선택한 버전으로 서비스합니다 (update.sh)">이 버전으로 적용</button> ' +
      '<span class="versions-switch-hint" aria-live="polite"></span>' +
      '</div></div>' +
      '</div>';
    var ipsDisplay = (host.host_ips && host.host_ips.length) ? host.host_ips.join(', ') : (host.host_ip || '-');
    var ipsAttr = (host.host_ips && host.host_ips.length) ? host.host_ips.join(',') : (host.host_ip || '');
    var primaryIp = host.host_ip || (host.host_ips && host.host_ips[0]) || '';
    var respondedFromDisplay = host.responded_from_ip || '-';
    div.setAttribute('data-cpu-uuid', host.cpu_uuid || '');
    div.setAttribute('data-hostname', host.hostname || '');
    div.setAttribute('data-host-ip', primaryIp);
    div.setAttribute('data-host-ips', ipsAttr);
    div.setAttribute('data-responded-from-ips', host.responded_from_ip || '');
    var hostDetailsDl = '<dl class="host-details">' +
      '<dt>CPU UUID</dt><dd>' + escapeHtml(host.cpu_uuid || '-') + '</dd>' +
      '<dt>버전</dt><dd>' + escapeHtml(host.version || '-') + (host.build_variant ? ' <span class="build-variant-badge">(' + escapeHtml(host.build_variant) + ')</span>' : '') + '</dd>' +
      '<dt>IP</dt><dd>' + escapeHtml(ipsDisplay) + '</dd>' +
      '<dt>응답한 IP</dt><dd>' + escapeHtml(respondedFromDisplay) + '</dd>' +
      '<dt>호스트명</dt><dd>' + escapeHtml(host.hostname || '-') + '</dd>' +
      '<dt>서비스 포트</dt><dd>' + (host.service_port != null ? host.service_port : '-') + '</dd>' +
      '<dt>CPU</dt><dd>' + escapeHtml(host.cpu_info || '-') + (host.cpu_usage_percent != null ? ' (' + host.cpu_usage_percent.toFixed(1) + '%)' : '') + '</dd>' +
      '<dt>메모리</dt><dd>' + formatMemory(host) + '</dd>' +
      '</dl>';
    var topContent = '<div class="updating-indicator" role="status" aria-label="업데이트 적용 중"></div>' +
      '<div class="host-icon">' + serverIconSvg + '</div>' +
      hostDetailsDl +
      (isSelf ? rightColumnSelf : rightColumnRemote);
    var remoteHealthRow = isSelf ? '' : (
      '<div class="remote-health-row">' +
      '<div class="discovery-miss-banner card-discovery-miss-banner" role="status" aria-live="polite" hidden>이번 Discovery에서 UDP 응답이 없었습니다. 이전에 발견된 호스트입니다.</div>' +
      '<div class="remote-health-banner remote-health-warn" role="alert" aria-live="polite" hidden></div>' +
      '<button type="button" class="service-btn remote-health-recheck-btn" hidden>헬스 수동 확인</button>' +
      '</div>');
    div.innerHTML = remoteHealthRow + '<div class="self-card-top">' + topContent + '</div>' + statusRowHtml;
    bindStatusToggle(div);
    return div;
  }

  function parseActiveFromOutput(output) {
    if (!output) return false;
    return /Active:\s*active\s*\(running\)/i.test(output);
  }

  function updateStatusUI(cardEl, output, summaryText) {
    if (!cardEl) return;
    var summary = cardEl.querySelector('.service-status-summary');
    var pre = cardEl.querySelector('.service-status-output');
    if (summary) summary.textContent = summaryText;
    if (pre) pre.textContent = output || '';
    var isKnownState = summaryText === '[정상 서비스 상태]' || summaryText === '[서비스 중지 상태]';
    if (isKnownState) {
      var startBtn = cardEl.querySelector('.service-start:not(.apply-update-host)');
      var active = parseActiveFromOutput(output);
      if (startBtn) startBtn.disabled = active;
      var row = cardEl.closest && cardEl.closest('.host-row');
      if (row) updateHostRowDot(row, active);
    }
  }

  function bindStatusToggle(cardEl) {
    var block = cardEl && cardEl.querySelector('.service-status-block');
    var header = cardEl && cardEl.querySelector('.service-status-header');
    var icon = cardEl && cardEl.querySelector('.service-status-icon');
    if (!block || !header) return;
    header.addEventListener('click', function () {
      var expanded = block.classList.toggle('expanded');
      header.setAttribute('aria-expanded', expanded);
      if (icon) icon.textContent = expanded ? '▼' : '▶';
    });
    header.addEventListener('keydown', function (e) {
      if (e.key === 'Enter' || e.key === ' ') {
        e.preventDefault();
        header.click();
      }
    });
  }

  function bindServiceControlButtons(cardEl) {
    if (!cardEl) return;
    var ip = cardEl.getAttribute('data-host-ip') || '';
    var summary = cardEl.querySelector('.service-status-summary');
    var isSelf = cardEl.classList.contains('self-card');
    var refreshBtn = cardEl.querySelector('.status-refresh-btn');
    if (refreshBtn) {
      refreshBtn.addEventListener('click', function () {
        if (summary) summary.textContent = '갱신 중…';
        var url = isSelf ? (API_BASE + '/self') : (API_BASE + '/host-info?ip=' + encodeURIComponent(ip));
        fetch(url)
          .then(function (res) { return res.json(); })
          .then(function (body) {
            if (body.status === 'success' && body.data) {
              updateHostCardDetails(cardEl, body.data);
              if (!isSelf) {
                fetchUpdateStatusForRemote(ip);
                updateAllHostApplyButtons();
              }
            } else {
              if (summary) summary.textContent = isSelf ? (body.data || '내 정보를 불러올 수 없습니다.') : (body.data || '호스트 정보를 불러올 수 없습니다.');
            }
            fetchServiceStatus(cardEl, isSelf ? '' : ip);
          })
          .catch(function () {
            if (summary) summary.textContent = isSelf ? '내 정보 요청 실패.' : '호스트 정보 요청 실패.';
            fetchServiceStatus(cardEl, isSelf ? '' : ip);
          });
      });
    }
    var restartBtn = cardEl.querySelector('.service-restart-btn');
    if (restartBtn) {
      restartBtn.addEventListener('click', function () {
        if (!isSelf && !isRemoteHostReachableForControl(cardEl)) {
          if (summary) summary.textContent = remoteHostUnreachableReason(cardEl);
          return;
        }
        if (summary) summary.textContent = '재시작 중…';
        var restartIp = isSelf ? 'self' : ip;
        function afterRestartMaybeRefresh() {
          if (summary) summary.textContent = '재시작되었습니다. 잠시 후 상태를 불러옵니다.';
          var delay = isSelf ? 2000 : 3500;
          var targetIp = isSelf ? '' : ip;
          setTimeout(function () {
            refreshHostCardDetails(cardEl, targetIp);
            fetchServiceStatus(cardEl, targetIp);
          }, delay);
        }
        function isRestartInProgressError(msg) {
          if (!msg || typeof msg !== 'string') return false;
          var s = msg.toLowerCase();
          return /terminated|connection reset|원격 재시작 요청 실패|eof/.test(s);
        }
        fetch(API_BASE + '/service-control', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ ip: restartIp, action: 'restart' })
        })
          .then(function (res) { return res.json(); })
          .then(function (body) {
            if (body.status === 'success') {
              afterRestartMaybeRefresh();
            } else {
              if (isRestartInProgressError(body.data)) {
                afterRestartMaybeRefresh();
              } else {
                if (summary) summary.textContent = body.data || '재시작 실패.';
                if (isSelf) fetchServiceStatus(cardEl, '');
              }
            }
          })
          .catch(function () {
            if (summary) summary.textContent = '재시작 요청을 보냈습니다. 잠시 후 상태를 불러옵니다.';
            afterRestartMaybeRefresh();
          });
      });
    }
    if (!isSelf) {
      var logRefreshBtn = cardEl.querySelector('.card-update-log-refresh-btn');
      if (logRefreshBtn) logRefreshBtn.addEventListener('click', function () { fetchUpdateLogForCard(cardEl, ip); });
      var configLoadBtn = cardEl.querySelector('.card-current-config-load-btn');
      var configSaveBtn = cardEl.querySelector('.card-current-config-save-btn');
      var configExpandBtn = cardEl.querySelector('.card-current-config-expand-btn');
      if (configLoadBtn) configLoadBtn.addEventListener('click', function () { fetchCurrentConfigForCard(cardEl, ip); });
      if (configSaveBtn) configSaveBtn.addEventListener('click', function () { saveCurrentConfigForCard(cardEl, ip); });
      if (configExpandBtn) configExpandBtn.addEventListener('click', function () { openConfigEditorModal(cardEl, ip); });
      var versionsRefreshBtn = cardEl.querySelector('.card-versions-list-refresh-btn');
      var versionsRemoveBtn = cardEl.querySelector('.card-versions-remove-btn');
      if (versionsRefreshBtn) versionsRefreshBtn.addEventListener('click', function () { fetchVersionsListForCard(cardEl, ip); });
      if (versionsRemoveBtn) versionsRemoveBtn.addEventListener('click', function () { doVersionsRemoveForCard(cardEl, ip); });
      var swSel = cardEl.querySelector('.card-versions-switch-select');
      var swBtn = cardEl.querySelector('.card-versions-switch-btn');
      if (swSel) {
        swSel.addEventListener('change', function () {
          updateVersionsSwitchButtonFromSelect(cardEl);
          setVersionsSwitchHint(cardEl, swSel.value);
        });
      }
      if (swBtn) swBtn.addEventListener('click', function () { doVersionsSwitch(cardEl, ip); });
    }
    if (isSelf) return;
    function doControl(action) {
      if (summary) summary.textContent = '갱신 중…';
      fetch(API_BASE + '/service-control', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ ip: ip, action: action })
      })
        .then(function (res) { return res.json(); })
        .then(function (body) {
          if (body.status === 'success') {
            fetchServiceStatus(cardEl, ip);
          } else {
            updateStatusUI(cardEl, null, body.data || '실패');
          }
        })
        .catch(function () {
          updateStatusUI(cardEl, null, '요청 실패');
        });
    }
    var startBtn = cardEl.querySelector('.service-start:not(.apply-update-host)');
    if (startBtn) startBtn.addEventListener('click', function () { doControl('start'); });

    var applyHostBtn = cardEl.querySelector('.apply-update-host');
    if (applyHostBtn) {
      applyHostBtn.addEventListener('click', function () {
        var card = applyHostBtn.closest && applyHostBtn.closest('.host-card');
        if (!isRemoteHostReachableForControl(card)) {
          if (summary) summary.textContent = remoteHostUnreachableReason(card);
          return;
        }
        var hostVersion = card ? (card.getAttribute('data-host-version') || '') : '';
        var stPre = remoteUpdateStatusByIP[ip];
        var canProceed;
        if (stPre && stPre.ok) {
          canProceed = !!(stPre.can_apply && stPre.apply_version);
        } else if (hasUploadableSelection()) {
          canProceed = true;
        } else {
          canProceed = canApplyToThisRemoteHostLegacy(hostVersion);
        }
        if (!canProceed) {
          if (summary) summary.textContent = '이 호스트에 적용할 스테이징 버전이 없거나 이미 동일 버전입니다.';
          return;
        }

        var reusePreviousConfig = getCardReusePreviousConfig(card);
        var reuseCheckboxVisible = isCardReuseConfigVisible(card);

        confirmApplyConfigChoice(reusePreviousConfig, reuseCheckboxVisible, function () {
          applyHostBtn.disabled = true;
          if (summary) summary.textContent = '업데이트 적용 중…';
          if (card) toggleCardVariantSelector(card, false);

          function recheckApplyButton() {
            var c = applyHostBtn.closest && applyHostBtn.closest('.host-card');
            var hip = c ? (c.getAttribute('data-host-ip') || '') : '';
            if (hip) {
              fetchUpdateStatusForRemote(hip);
            } else {
              updateAllHostApplyButtons();
            }
          }

          function doApplyToHost(version) {
            showCardUpdating(cardEl, true);
            fetch(API_BASE + '/apply-update', {
              method: 'POST',
              headers: { 'Content-Type': 'application/json' },
              body: JSON.stringify({
                version: version,
                ip: ip,
                agent_variant: getCardAgentVariant(cardEl),
                reuse_previous_config: reusePreviousConfig
              })
            })
              .then(function (res) { return res.json(); })
              .then(function (body) {
                if (body.status === 'success') {
                  scheduleRefreshAfterApply(cardEl, ip, summary, body.data, version);
                } else {
                  updateStatusUI(cardEl, null, body.data || '적용 실패');
                  showCardUpdating(cardEl, false);
                }
              })
              .catch(function () {
                updateStatusUI(cardEl, null, '요청 실패');
                showCardUpdating(cardEl, false);
              })
              .finally(recheckApplyButton);
          }

          var stV = remoteUpdateStatusByIP[ip];
          var applicableVersion = (stV && stV.ok && stV.apply_version) ? stV.apply_version : getApplicableVersion();
          if (applicableVersion) {
            doApplyToHost(applicableVersion);
            return;
          }
          // tar.gz 번들 선택 시: 로컬 스테이징 없이 원격으로만 전송 (multipart apply-update)
          var bundleInput = el('upload-bundle');
          if (!bundleInput || !bundleInput.files[0]) {
            if (summary) summary.textContent = '원격 적용할 tar.gz 번들을 선택하세요.';
            applyHostBtn.disabled = false;
            recheckApplyButton();
            return;
          }
          var formData = new FormData();
          formData.append('ip', ip);
          formData.append('bundle', bundleInput.files[0]);
          formData.append('agent_variant', getCardAgentVariant(cardEl));
          formData.append('reuse_previous_config', reusePreviousConfig ? 'true' : 'false');
          showCardUpdating(cardEl, true);
          fetch(API_BASE + '/apply-update', {
            method: 'POST',
            body: formData
          })
            .then(function (res) { return res.json(); })
            .then(function (body) {
              if (body.status === 'success') {
                var ver;
                if (body.data && typeof body.data === 'string') {
                  var m = body.data.match(/version\s+(\S+)\s+applied on remote/i);
                  if (m) ver = m[1];
                }
                scheduleRefreshAfterApply(cardEl, ip, summary, body.data, ver);
              } else {
                updateStatusUI(cardEl, null, body.data || '적용 실패');
                showCardUpdating(cardEl, false);
              }
            })
            .catch(function () {
              updateStatusUI(cardEl, null, '요청 실패');
              showCardUpdating(cardEl, false);
            })
            .finally(recheckApplyButton);
        });
      });
    }
    bindRemoteHealthForCard(cardEl);
  }

  function getApplicableVersion() {
    if (lastUpdateStatus.apply_version) {
      return lastUpdateStatus.apply_version;
    }
    if (lastUpdateStatus.staging_versions && lastUpdateStatus.staging_versions.length > 0) {
      return lastUpdateStatus.staging_versions[0];
    }
    return lastUploadedVersion || '';
  }

  /** Fallback when /update-status?ip= failed: approximate using newest staging vs card version (may disagree with server). */
  function canApplyToThisRemoteHostLegacy(hostVersion) {
    if (hasUploadableSelection()) return true;
    var applicable = getApplicableVersion();
    if (!applicable) return false;
    return applicable !== (hostVersion || '');
  }

  function getApplyButtonTitle(hostVersion, canApply, applicableVersion) {
    if (canApply && applicableVersion) {
      return applicableVersion + ' 버전으로 업데이트 가능합니다';
    }
    if (!applicableVersion) {
      return '먼저 업데이트 영역에서 버전을 업로드하세요';
    }
    return '최신 버전입니다';
  }

  /** After apply-update: reload panels. Skip update log while startUpdateLogPolling is active (avoid stale overwrite). */
  var activeLogPollVersion = '';

  function refreshAllPanelsAfterUpdate(cardEl, ip) {
    if (ip) {
      if (!cardEl) return;
      if (!activeLogPollVersion) fetchUpdateLogForCard(cardEl, ip);
      fetchCurrentConfigForCard(cardEl, ip);
      fetchVersionsListForCard(cardEl, ip);
      fetchServiceStatus(cardEl, ip);
      fetchUpdateStatusForRemote(ip);
    } else {
      if (!activeLogPollVersion) fetchUpdateLog(true);
      fetchCurrentConfig();
      fetchVersionsList();
      if (cardEl) fetchServiceStatus(cardEl, '');
    }
    fetchUpdateStatus();
  }

  /**
   * Poll url until JSON returns status success + data, or maxAttempts exhausted.
   * onGiveUp(networkFailure): networkFailure true if last failure was fetch/parse error.
   */
  function pollUntilHostJsonOk(url, maxAttempts, firstDelayMs, retryDelayMs, onOk, onGiveUp) {
    function step(attempt) {
      setTimeout(function () {
        fetch(url)
          .then(function (res) { return res.json(); })
          .then(function (body) {
            if (body.status === 'success' && body.data) {
              onOk(body);
              return;
            }
            if (attempt + 1 < maxAttempts) step(attempt + 1);
            else if (onGiveUp) onGiveUp(false);
          })
          .catch(function () {
            if (attempt + 1 < maxAttempts) step(attempt + 1);
            else if (onGiveUp) onGiveUp(true);
          });
      }, attempt === 0 ? firstDelayMs : retryDelayMs);
    }
    step(0);
  }

  function scheduleRefreshAfterApply(cardEl, ip, summary, successMessage, appliedVersion, onDone, opts) {
    opts = opts || {};
    if (summary && !opts.skipInitialSummary) {
      summary.textContent = successMessage || '적용 완료. 잠시 후 상태를 다시 읽어옵니다.';
    }
    if (appliedVersion && cardEl) {
      cardEl.setAttribute('data-host-version', appliedVersion);
      var dds = cardEl.querySelectorAll('.host-details > dd');
      if (dds && dds.length >= 8) {
        dds[1].textContent = appliedVersion;
      }
      startUpdateLogPolling(appliedVersion, cardEl, ip || '');
    }
    var url = API_BASE + '/host-info?ip=' + encodeURIComponent(ip);
    pollUntilHostJsonOk(url, 8, 5000, 2000, function (body) {
      updateHostCardDetails(cardEl, body.data);
      refreshAllPanelsAfterUpdate(cardEl, ip);
      if (summary) summary.textContent = successMessage || '적용 완료. 업데이트 기록·config·버전·상태를 반영했습니다.';
      showCardUpdating(cardEl, false);
      if (onDone) onDone();
    }, function () {
      refreshAllPanelsAfterUpdate(cardEl, ip);
      showCardUpdating(cardEl, false);
      if (onDone) onDone();
    });
  }

  /** After switch-current: same panel refresh as apply-update (log, config, versions, service, update-status). */
  function scheduleRefreshAfterSwitchCurrent(cardEl, ip, switchedVersion, statusEl) {
    if (!ip) {
      var selfCard = el('self-info') && el('self-info').querySelector('.host-card');
      if (selfCard) showCardUpdating(selfCard, true);
      if (statusEl) statusEl.textContent = '전환 반영 중… 재시작 후 정보를 자동으로 불러옵니다.';
      startUpdateLogPolling(switchedVersion, selfCard, '');
      fetchUpdateStatus();
      pollUntilHostJsonOk(API_BASE + '/self', 15, 4000, 2000, function (body2) {
        if (selfCard) updateHostCardDetails(selfCard, body2.data);
        refreshAllPanelsAfterUpdate(selfCard, '');
        updateAllHostApplyButtons();
        if (selfCard) showCardUpdating(selfCard, false);
        if (statusEl) statusEl.textContent = '전환 완료. 업데이트 기록·config·버전·상태를 반영했습니다.';
        fetchUpdateStatus();
        updateVersionsSwitchButtonFromSelect(null);
      }, function (networkFailure) {
        refreshAllPanelsAfterUpdate(selfCard, '');
        if (selfCard) showCardUpdating(selfCard, false);
        if (statusEl) {
          statusEl.textContent = networkFailure
            ? '연결 실패. 페이지를 새로고침해 보세요.'
            : '서버 응답이 지연됩니다. 잠시 후 정보를 새로고침하세요.';
        }
        fetchUpdateStatus();
        updateVersionsSwitchButtonFromSelect(null);
      });
      return;
    }
    if (cardEl) showCardUpdating(cardEl, true);
    if (statusEl) statusEl.textContent = '전환 반영 중… 잠시 후 상태를 다시 읽어옵니다.';
    scheduleRefreshAfterApply(
      cardEl,
      ip,
      statusEl,
      '전환 완료. 업데이트 기록·config·버전·상태를 반영했습니다.',
      switchedVersion,
      function () {
        updateVersionsSwitchButtonFromSelect(cardEl);
        fetchUpdateStatus();
      },
      { skipInitialSummary: true }
    );
  }

  function updateAllHostApplyButtons() {
    var localApplicable = getApplicableVersion();
    var btns = document.querySelectorAll('.apply-update-host');
    /** Remote card: show compute/control only when apply is enabled and dual-agent (or file multipart) applies. */
    function showRemoteVariantSelector(btn, card) {
      if (!btn || !card || btn.disabled) return false;
      if (card.classList.contains('is-updating')) return false;
      return !!(lastUpdateStatus.staging_dual_agents || hasUploadableSelection());
    }
    for (var i = 0; i < btns.length; i++) {
      var btn = btns[i];
      var card = btn.closest && btn.closest('.host-card');
      if (!card) continue;
      if (!card.classList.contains('self-card') && !isRemoteHostReachableForControl(card)) {
        btn.disabled = true;
        btn.title = remoteHostUnreachableReason(card);
        toggleCardVariantSelector(card, false);
        continue;
      }
      var hostVersion = card.getAttribute('data-host-version') || '';
      var ip = card.getAttribute('data-host-ip') || '';
      var st = remoteUpdateStatusByIP[ip];

      if (!st || st.pending) {
        btn.disabled = true;
        btn.title = '로컬 스테이징과 원격 버전 비교 중…';
        toggleCardVariantSelector(card, false);
        continue;
      }

      var applicableVersion;
      var canApply;
      if (st.ok) {
        canApply = !!(st.can_apply && st.apply_version);
        applicableVersion = st.apply_version || '';
        btn.disabled = !canApply;
        if (!canApply) {
          var rv = st.remote_version || hostVersion || '';
          if (applicableVersion && rv && applicableVersion === rv) {
            btn.title = '원격이 이미 스테이징 버전(' + applicableVersion + ')과 같습니다. 동일 버전 재적용은 AllowSameVersionUpdate가 필요합니다.';
          } else {
            btn.title = getApplyButtonTitle(hostVersion, false, applicableVersion || localApplicable);
          }
        } else {
          btn.title = getApplyButtonTitle(hostVersion, true, applicableVersion || localApplicable);
        }
        toggleCardVariantSelector(card, showRemoteVariantSelector(btn, card));
        continue;
      }

      if (hasUploadableSelection()) {
        btn.disabled = false;
        btn.title = localApplicable
          ? (localApplicable + ' 번들로 원격 전송 가능')
          : 'tar.gz 번들로 원격 적용';
        toggleCardVariantSelector(card, showRemoteVariantSelector(btn, card));
        continue;
      }

      applicableVersion = localApplicable;
      canApply = canApplyToThisRemoteHostLegacy(hostVersion);
      btn.disabled = !canApply;
      btn.title = (st && st.err ? '원격 상태 확인 실패 — 표시는 추정입니다. ' : '') +
        getApplyButtonTitle(hostVersion, canApply, applicableVersion);
      toggleCardVariantSelector(card, showRemoteVariantSelector(btn, card));
    }
    updateReuseConfigVisibility();
    updateAllRemoteServiceControlButtons();
  }

  function toggleCardVariantSelector(card, show) {
    if (!card) return;
    var sel = card.querySelector('.card-variant-selector');
    if (!sel) return;
    sel.hidden = !show;
    if (show) {
      setVariantRadioSelection(sel, card.getAttribute('data-build-variant'));
    }
  }

  function doRemoveUpload() {
    var version = lastUpdateStatus.remove_version;
    if (!version) return;
    var status = el('upload-status');
    var removeBtn = el('remove-upload-btn');
    if (removeBtn) removeBtn.disabled = true;
    status.textContent = '스테이징에서 버전 삭제 중…';
    fetch(API_BASE + '/upload/remove', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ version: version })
    })
      .then(function (res) { return res.json(); })
      .then(function (body) {
        if (body.status === 'success') {
          status.textContent = body.data || '스테이징에서 삭제되었습니다.';
          lastUpdateStatus = {
            can_apply: false,
            apply_version: '',
            staging_versions: [],
            remove_version: '',
            staging_dual_agents: false
          };
          var variantFs = el('agent-variant-fieldset');
          if (variantFs) variantFs.hidden = true;
          var applyBtn = el('apply-update-btn');
          if (applyBtn) applyBtn.disabled = true;
          var stagingDisplay = el('staging-version-display');
          if (stagingDisplay) stagingDisplay.textContent = '';
          updateReuseConfigVisibility();
          updateAllHostApplyButtons();
          fetchUpdateStatus();
        } else {
          status.textContent = body.data || '삭제 실패.';
          fetchUpdateStatus();
        }
      })
      .catch(function () {
        status.textContent = '요청 실패.';
        fetchUpdateStatus();
      });
  }

  function fetchServiceStatus(cardEl, ip) {
    var summary = cardEl && cardEl.querySelector('.service-status-summary');
    if (!summary) return;
    var url = API_BASE + '/service-status';
    if (ip) {
      url += '?ip=' + encodeURIComponent(ip);
    }
    fetch(url)
      .then(function (res) { return res.json(); })
      .then(function (body) {
        if (body.status === 'success' && body.data && body.data.output) {
          var output = body.data.output;
          var active = parseActiveFromOutput(output);
          var label = active ? '[정상 서비스 상태]' : '[서비스 중지 상태]';
          updateStatusUI(cardEl, output, label);
        } else {
          updateStatusUI(cardEl, body.data || '상태를 불러올 수 없습니다.', body.data || '상태를 불러올 수 없습니다.');
        }
      })
      .catch(function () {
        updateStatusUI(cardEl, null, '상태를 불러올 수 없습니다.');
      });
  }

  function escapeHtml(s) {
    if (s == null) return '';
    const t = document.createElement('div');
    t.textContent = s;
    return t.innerHTML;
  }

  var UPDATE_LOG_ROLLBACK_WARNING_HTML = '<span class="update-warning-title">⚠ 최근 업데이트 실패·롤백</span><br><span class="update-warning-desc">위 기록에서 failed 또는 rollback 항목을 확인하세요.</span>';

  function fetchUpdateLogJSON(ip) {
    var url = API_BASE + '/update-log';
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
        pre = el('self-update-log-output');
        warningEl = el('self-update-rollback-warning');
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
      cardEl.setAttribute('data-build-variant', defaultAgentVariantFromBuild(host.build_variant));
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
    if (row) updateHostRowLabel(row, host, cardEl.classList.contains('self-card'));
    var variantSel = cardEl.querySelector('.card-variant-selector');
    if (variantSel && !variantSel.hidden) {
      setVariantRadioSelection(variantSel, cardEl.getAttribute('data-build-variant'));
    }
    if (cardEl.classList.contains('self-card')) {
      applyLocalVariantDefault();
    }
  }

  function refreshHostCardDetails(cardEl, ip) {
    if (!cardEl) return;
    var url = (ip === '') ? (API_BASE + '/self') : (API_BASE + '/host-info?ip=' + encodeURIComponent(ip));
    fetch(url)
      .then(function (res) { return res.json(); })
      .then(function (body) {
        if (body.status === 'success' && body.data) {
          updateHostCardDetails(cardEl, body.data);
          if (ip !== '') updateAllHostApplyButtons();
        }
      })
      .catch(function () {});
  }

  function loadSelf() {
    const container = el('self-info');
    container.innerHTML = '<div class="host-loading">불러오는 중…</div>';
    fetch(API_BASE + '/self')
      .then(function (res) { return res.json(); })
      .then(function (body) {
        if (body.status === 'success' && body.data) {
          container.innerHTML = '';
          var row = renderHostRow(body.data, true);
          container.appendChild(row);
          var card = row.querySelector('.host-card');
          bindServiceControlButtons(card);
          fetchServiceStatus(card, '');
          var logRefreshBtn = el('self-update-log-refresh-btn');
          var versionsRefreshBtn = el('self-versions-list-refresh-btn');
          var versionsRemoveBtn = el('self-versions-remove-btn');
          if (logRefreshBtn) logRefreshBtn.addEventListener('click', function () { fetchUpdateLog(); });
          if (versionsRefreshBtn) versionsRefreshBtn.addEventListener('click', fetchVersionsList);
          if (versionsRemoveBtn) versionsRemoveBtn.addEventListener('click', doVersionsRemove);
          var swSelSelf = el('self-versions-switch-select');
          var swBtnSelf = el('self-versions-switch-btn');
          if (swSelSelf) {
            swSelSelf.addEventListener('change', function () {
              updateVersionsSwitchButtonFromSelect(null);
              setVersionsSwitchHint(null, swSelSelf.value);
            });
          }
          if (swBtnSelf) swBtnSelf.addEventListener('click', function () { doVersionsSwitch(null, ''); });
          var configLoadBtn = el('self-current-config-load-btn');
          var configSaveBtn = el('self-current-config-save-btn');
          var configExpandBtn = el('self-current-config-expand-btn');
          if (configLoadBtn) configLoadBtn.addEventListener('click', fetchCurrentConfig);
          if (configSaveBtn) configSaveBtn.addEventListener('click', saveCurrentConfig);
          if (configExpandBtn) configExpandBtn.addEventListener('click', function () { openConfigEditorModal(null, ''); });
          fetchUpdateLog();
          fetchCurrentConfig();
          fetchVersionsList();
        } else {
          container.innerHTML = '<div class="host-error">내 정보를 불러올 수 없습니다.</div>';
        }
      })
      .catch(function () {
        container.innerHTML = '<div class="host-error">내 정보를 불러올 수 없습니다.</div>';
      });
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

  function countRemoteHostCards() {
    var list = el('discovered-hosts');
    if (!list) return 0;
    return list.querySelectorAll('.host-card:not(.self-card)').length;
  }

  function collectRemoteHostsFromDOM() {
    var list = el('discovered-hosts');
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

  var lastPushBulkResults = { lines: [], title: '' };
  var lastRestartBulkResults = { lines: [], title: '' };
  var lastApplyUpdateBulkResults = { lines: [], title: '' };
  var lastRollbackBulkResults = { lines: [], title: '' };

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
    var modal = el('bulk-remote-results-modal');
    var title = el('bulk-remote-results-modal-title');
    var body = el('bulk-remote-results-body');
    var data = store || { lines: [], title: '' };
    if (title) title.textContent = data.title || '결과';
    if (body) body.textContent = (data.lines && data.lines.length) ? data.lines.join('\n') : '(결과 없음)';
    if (modal) {
      modal.hidden = false;
      modal.setAttribute('aria-hidden', 'false');
    }
    document.body.classList.add('config-modal-open');
  }

  function closeBulkResultsModal() {
    var modal = el('bulk-remote-results-modal');
    if (modal) {
      modal.hidden = true;
      modal.setAttribute('aria-hidden', 'true');
    }
    document.body.classList.remove('config-modal-open');
  }

  function initBulkResultsModal() {
    var closeBtn = el('bulk-remote-results-modal-close-btn');
    var dismissBtn = el('bulk-remote-results-modal-dismiss-btn');
    var backdrop = el('bulk-remote-results-modal') && el('bulk-remote-results-modal').querySelector('.config-editor-modal-backdrop');
    if (closeBtn) closeBtn.addEventListener('click', closeBulkResultsModal);
    if (dismissBtn) dismissBtn.addEventListener('click', closeBulkResultsModal);
    if (backdrop) backdrop.addEventListener('click', closeBulkResultsModal);
    var pushResultsBtn = el('push-config-all-results-btn');
    var restartResultsBtn = el('restart-all-results-btn');
    if (pushResultsBtn) {
      pushResultsBtn.addEventListener('click', function () {
        openBulkResultsModal(lastPushBulkResults);
      });
    }
    if (restartResultsBtn) {
      restartResultsBtn.addEventListener('click', function () {
        openBulkResultsModal(lastRestartBulkResults);
      });
    }
    var applyUpdateResultsBtn = el('apply-update-all-results-btn');
    var rollbackResultsBtn = el('rollback-all-results-btn');
    if (applyUpdateResultsBtn) {
      applyUpdateResultsBtn.addEventListener('click', function () {
        openBulkResultsModal(lastApplyUpdateBulkResults);
      });
    }
    if (rollbackResultsBtn) {
      rollbackResultsBtn.addEventListener('click', function () {
        openBulkResultsModal(lastRollbackBulkResults);
      });
    }
  }

  function syncBulkRemoteStatusDismiss(statusEl, dismissEl) {
    if (!dismissEl) return;
    var hasText = statusEl && String(statusEl.textContent || '').trim().length > 0;
    dismissEl.hidden = !hasText;
  }

  function initBulkRemoteStatusDismiss() {
    [
      {
        statusId: 'push-config-all-status',
        dismissId: 'push-config-all-status-dismiss-btn',
        resultsId: 'push-config-all-results-btn'
      },
      {
        statusId: 'restart-all-status',
        dismissId: 'restart-all-status-dismiss-btn',
        resultsId: 'restart-all-results-btn'
      },
      {
        statusId: 'apply-update-all-status',
        dismissId: 'apply-update-all-status-dismiss-btn',
        resultsId: 'apply-update-all-results-btn'
      },
      {
        statusId: 'rollback-all-status',
        dismissId: 'rollback-all-status-dismiss-btn',
        resultsId: 'rollback-all-results-btn'
      }
    ].forEach(function (pair) {
      var statusEl = el(pair.statusId);
      var dismissBtn = el(pair.dismissId);
      var resultsBtn = el(pair.resultsId);
      if (!statusEl || !dismissBtn) return;
      dismissBtn.addEventListener('click', function () {
        statusEl.textContent = '';
        dismissBtn.hidden = true;
        if (resultsBtn) resultsBtn.hidden = true;
      });
    });
  }

  function refreshRemoteBulkButtonsState() {
    var domCount = countRemoteHostCards();
    var noHostsTitle = domCount === 0 ? 'Discovery로 원격 호스트를 먼저 찾으세요.' : '';
    ['push-config-all-remotes-btn', 'restart-all-remotes-btn', 'rollback-all-remotes-btn'].forEach(function (id) {
      var btn = el(id);
      if (!btn || btn.getAttribute('data-busy') === '1') return;
      btn.disabled = domCount === 0;
      btn.title = noHostsTitle;
    });
    var applyAllBtn = el('apply-update-all-remotes-btn');
    if (applyAllBtn && applyAllBtn.getAttribute('data-busy') !== '1') {
      var canApply = !!(lastUpdateStatus && lastUpdateStatus.can_apply && lastUpdateStatus.apply_version);
      applyAllBtn.disabled = domCount === 0 || !canApply;
      if (domCount === 0) {
        applyAllBtn.title = noHostsTitle;
      } else if (!canApply) {
        applyAllBtn.title = '스테이징에 적용 가능한 버전이 없습니다.';
      } else {
        applyAllBtn.title = '';
      }
    }
  }

  function runBulkHostsNDJSON(options) {
    var btn = options.buttonEl;
    var statusEl = options.statusEl;
    var dismissEl = options.dismissEl;
    var resultsBtn = options.resultsBtnEl;
    var label = options.label;
    var apiPath = options.apiPath;
    var formatLine = options.formatLine;
    var formatSummary = options.formatSummary;
    var resultsTitle = options.resultsTitle;
    var onDone = options.onDone || function () {};
    var resultsStore = options.resultsStore;
    if (!btn || btn.disabled || btn.getAttribute('data-busy') === '1') return;
    btn.setAttribute('data-busy', '1');
    btn.disabled = true;
    if (statusEl) statusEl.textContent = '';
    if (dismissEl) dismissEl.hidden = true;
    if (resultsBtn) resultsBtn.hidden = true;
    if (resultsStore) {
      resultsStore.lines = [];
      resultsStore.title = resultsTitle;
    }

    var hosts = collectRemoteHostsFromDOM();
    var requestBody = options.buildRequestBody
      ? options.buildRequestBody(hosts)
      : { hosts: hosts };
    fetch(API_BASE + apiPath, {
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
          btn.textContent = label;
          btn.removeAttribute('data-busy');
          if (resultsStore) {
            resultsStore.lines = progressResults.map(formatLine);
          }
          if (resultsBtn) resultsBtn.hidden = progressResults.length === 0;
          if (statusEl) {
            if (evt && evt.total === 0) {
              statusEl.textContent = hosts.length
                ? '원격 호스트에 연결할 수 없습니다. Discovery를 다시 실행해 보세요.'
                : (options.emptyText || '대상 호스트가 없습니다. Discovery를 실행하세요.');
            } else if (evt) {
              statusEl.textContent = formatSummary(evt) || '요청이 중단되었습니다.';
            } else {
              statusEl.textContent = '요청이 중단되었습니다.';
            }
            syncBulkRemoteStatusDismiss(statusEl, dismissEl);
          }
          refreshRemoteBulkButtonsState();
          onDone(evt, progressResults);
          fetchUpdateLog(true);
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
        if (statusEl) statusEl.textContent = '요청 실패.';
        syncBulkRemoteStatusDismiss(statusEl, dismissEl);
        refreshRemoteBulkButtonsState();
      });
  }

  function refreshRemoteConfigEditorsAfterBulkPush() {
    var list = el('discovered-hosts');
    if (!list) return;
    var cards = list.querySelectorAll('.host-card:not(.self-card)');
    for (var i = 0; i < cards.length; i++) {
      var card = cards[i];
      var ip = card.getAttribute('data-host-ip');
      if (ip) fetchCurrentConfigForCard(card, ip);
    }
  }

  function runPushConfigToAllRemotes() {
    runBulkHostsNDJSON({
      buttonEl: el('push-config-all-remotes-btn'),
      statusEl: el('push-config-all-status'),
      dismissEl: el('push-config-all-status-dismiss-btn'),
      resultsBtnEl: el('push-config-all-results-btn'),
      resultsStore: lastPushBulkResults,
      label: PUSH_CONFIG_ALL_LABEL,
      apiPath: '/current-config/push-local-all',
      formatLine: formatPushConfigResultLine,
      formatSummary: function (evt) { return formatBulkSummary(evt, '대 모두 복사했습니다.'); },
      resultsTitle: '설정 일괄 복사 결과',
      emptyText: '복사할 원격 호스트가 없습니다. Discovery를 실행하세요.',
      onDone: function () {
        refreshRemoteConfigEditorsAfterBulkPush();
        var list = el('discovered-hosts');
        if (!list) return;
        var cards = list.querySelectorAll('.host-card:not(.self-card)');
        for (var i = 0; i < cards.length; i++) {
          fetchServiceStatus(cards[i], cards[i].getAttribute('data-host-ip') || '');
        }
      }
    });
  }

  function runRestartAllRemotes() {
    runBulkHostsNDJSON({
      buttonEl: el('restart-all-remotes-btn'),
      statusEl: el('restart-all-status'),
      dismissEl: el('restart-all-status-dismiss-btn'),
      resultsBtnEl: el('restart-all-results-btn'),
      resultsStore: lastRestartBulkResults,
      label: RESTART_ALL_LABEL,
      apiPath: '/service-control/restart-all',
      formatLine: formatRestartResultLine,
      formatSummary: function (evt) { return formatBulkSummary(evt, '대 모두 재시작되었습니다.'); },
      resultsTitle: '리모트 일괄 재시작 결과',
      emptyText: '재시작할 원격 호스트가 없습니다. Discovery를 실행하세요.',
      onDone: function () {
        var list = el('discovered-hosts');
        if (!list) return;
        var cards = list.querySelectorAll('.host-card:not(.self-card)');
        for (var i = 0; i < cards.length; i++) {
          var card = cards[i];
          var ip = card.getAttribute('data-host-ip') || '';
          if (ip) {
            fetchServiceStatus(card, ip);
            registerRemoteHealthMonitoring(card);
          }
        }
      }
    });
  }

  function runApplyUpdateAllRemotes() {
    if (!lastUpdateStatus.can_apply || !lastUpdateStatus.apply_version) return;
    runBulkHostsNDJSON({
      buttonEl: el('apply-update-all-remotes-btn'),
      statusEl: el('apply-update-all-status'),
      dismissEl: el('apply-update-all-status-dismiss-btn'),
      resultsBtnEl: el('apply-update-all-results-btn'),
      resultsStore: lastApplyUpdateBulkResults,
      label: APPLY_UPDATE_ALL_LABEL,
      apiPath: '/apply-update-all',
      buildRequestBody: function (hosts) {
        return {
          hosts: hosts,
          version: lastUpdateStatus.apply_version || '',
          agent_variant: getSelectedAgentVariant(),
          reuse_previous_config: getReusePreviousConfig()
        };
      },
      formatLine: formatApplyUpdateResultLine,
      formatSummary: function (evt) {
        return formatBulkDoneSummary(evt, '대 모두에 업데이트를 적용했습니다.');
      },
      resultsTitle: '리모트 일괄 업데이트 적용 결과',
      emptyText: '적용할 원격 호스트가 없습니다. Discovery를 실행하세요.',
      onDone: function () {
        fetchUpdateStatus();
        var list = el('discovered-hosts');
        if (!list) return;
        var cards = list.querySelectorAll('.host-card:not(.self-card)');
        for (var i = 0; i < cards.length; i++) {
          var card = cards[i];
          var ip = card.getAttribute('data-host-ip') || '';
          if (ip) {
            fetchServiceStatus(card, ip);
            fetchUpdateLogForCard(card, ip);
            registerRemoteHealthMonitoring(card);
          }
        }
      }
    });
  }

  function runRollbackAllRemotes() {
    runBulkHostsNDJSON({
      buttonEl: el('rollback-all-remotes-btn'),
      statusEl: el('rollback-all-status'),
      dismissEl: el('rollback-all-status-dismiss-btn'),
      resultsBtnEl: el('rollback-all-results-btn'),
      resultsStore: lastRollbackBulkResults,
      label: ROLLBACK_ALL_LABEL,
      apiPath: '/versions/rollback-all',
      formatLine: formatRollbackResultLine,
      formatSummary: function (evt) {
        return formatBulkDoneSummary(evt, '대 모두 롤백했습니다.');
      },
      resultsTitle: '리모트 일괄 롤백 결과',
      emptyText: '롤백할 원격 호스트가 없습니다. Discovery를 실행하세요.',
      onDone: function () {
        fetchUpdateStatus();
        var list = el('discovered-hosts');
        if (!list) return;
        var cards = list.querySelectorAll('.host-card:not(.self-card)');
        for (var i = 0; i < cards.length; i++) {
          var card = cards[i];
          var ip = card.getAttribute('data-host-ip') || '';
          if (ip) {
            fetchServiceStatus(card, ip);
            fetchUpdateLogForCard(card, ip);
            fetchVersionsListForCard(card, ip);
            registerRemoteHealthMonitoring(card);
          }
        }
      }
    });
  }

  function runDiscovery() {
    const btn = el('discovery-btn');
    const status = el('discovery-status');
    const list = el('discovered-hosts');
    if (!list) return;
    stopDiscoveryCountdown();
    btn.disabled = true;
    var count = list.querySelectorAll('.host-card:not(.self-card)').length;
    var discoveryRespondedKeys = {};
    startDiscoveryCountdown(status, count);
    var discoveryFailHandled = false;
    var evtSource = new EventSource(API_BASE + '/discovery/stream');
    evtSource.addEventListener('discoveryfail', function (e) {
      discoveryFailHandled = true;
      stopDiscoveryCountdown();
      try {
        var j = JSON.parse(e.data);
        status.textContent = 'Discovery 요청 실패: ' + (j.message || e.data);
      } catch (err) {
        status.textContent = 'Discovery 요청 실패: ' + (e.data || '');
      }
      evtSource.close();
      btn.disabled = false;
      updateAllHostApplyButtons();
    });
    evtSource.onmessage = function (e) {
      try {
        var host = JSON.parse(e.data);
        if (host.self) {
          var selfCard = el('self-info') && el('self-info').querySelector('.host-card');
          if (selfCard && host.responded_from_ip) {
            mergeRespondedFromIntoCard(selfCard, host.responded_from_ip);
            var selfRow = el('self-info').querySelector('.host-row');
            if (selfRow) updateHostRowLabel(selfRow, { hostname: host.hostname || selfCard.getAttribute('data-hostname') || '', responded_from_ip: host.responded_from_ip }, true);
          }
          return;
        }
        var ip = host.host_ip || '';
        var cpuUuid = (host.cpu_uuid || '').trim();
        var existing = null;
        if (cpuUuid) existing = findHostCardByCpuUuid(list, cpuUuid);
        if (!existing && ip) existing = findHostCardByIp(list, ip);
        var trackKey = discoveryTrackKeyFromHost(host);
        if (trackKey) discoveryRespondedKeys[trackKey] = true;
        /* hostname으로는 기존 카드를 찾지 않음: 서로 다른 호스트가 같은 hostname(예: kt-vm)을 쓰면 한 카드로 잘못 병합됨 */
        if (existing) {
          if (cpuUuid) existing.setAttribute('data-cpu-uuid', cpuUuid);
          if (host.hostname) existing.setAttribute('data-hostname', host.hostname);
          mergeHostIpsFromResponseIntoCard(existing, host);
          if (host.responded_from_ip) mergeRespondedFromIntoCard(existing, host.responded_from_ip);
          updateHostCardDetails(existing, host);
          var row = existing.closest && existing.closest('.host-row');
          if (row) updateHostRowLabel(row, host, false);
          var primaryIp = existing.getAttribute('data-host-ip') || ip;
          fetchServiceStatus(existing, primaryIp);
          fetchUpdateStatusForRemote(primaryIp);
          updateAllHostApplyButtons();
          registerRemoteHealthMonitoring(existing);
          setDiscoveryMissUI(row, false);
        } else {
          var row = renderHostRow(host, false);
          list.appendChild(row);
          refreshRemoteHostRowTints();
          var card = row.querySelector('.host-card');
          bindServiceControlButtons(card);
          fetchServiceStatus(card, ip);
          fetchUpdateStatusForRemote(ip);
          registerRemoteHealthMonitoring(card);
          setDiscoveryMissUI(row, false);
        }
        count = list.querySelectorAll('.host-card:not(.self-card)').length;
        refreshDiscoveryProgressStatus(status, count);
      } catch (err) {}
    };
    evtSource.addEventListener('done', function () {
      evtSource.close();
      stopDiscoveryCountdown();
      btn.disabled = false;
      var missedCount = finalizeDiscoveryMissMarks(list, discoveryRespondedKeys);
      var respondedCount = 0;
      for (var k in discoveryRespondedKeys) {
        if (Object.prototype.hasOwnProperty.call(discoveryRespondedKeys, k)) respondedCount++;
      }
      count = list.querySelectorAll('.host-card:not(.self-card)').length;
      status.textContent = formatDiscoveryDoneStatus(count, respondedCount, missedCount);
      refreshRemoteHostRowTints();
      fetchUpdateStatusForAllRemoteHosts();
      updateAllHostApplyButtons();
      refreshRemoteBulkButtonsState();
    });
    evtSource.onerror = function () {
      evtSource.close();
      stopDiscoveryCountdown();
      btn.disabled = false;
      if (discoveryFailHandled) {
        updateAllHostApplyButtons();
        return;
      }
      if (count === 0) {
        status.textContent = 'Discovery 요청 실패 (서버 연결 오류 또는 스트림 중단). journalctl -u contrabass-mole.service 로 서버 로그를 확인하세요.';
      } else {
        var missedCount = finalizeDiscoveryMissMarks(list, discoveryRespondedKeys);
        var respondedCount = 0;
        for (var k in discoveryRespondedKeys) {
          if (Object.prototype.hasOwnProperty.call(discoveryRespondedKeys, k)) respondedCount++;
        }
        status.textContent = formatDiscoveryDoneStatus(count, respondedCount, missedCount);
        refreshRemoteHostRowTints();
      }
      updateAllHostApplyButtons();
      refreshRemoteBulkButtonsState();
    };
  }

  var lastUploadedVersion = '';
  var lastUpdateStatus = { can_apply: false, apply_version: '', staging_versions: [], remove_version: '' };

  /** Per remote host: GET /update-status?ip= — same StagingUpdateAvailable logic as server (local staging vs that host's running version). */
  var remoteUpdateStatusByIP = {};

  function fetchUpdateStatusForRemote(ip) {
    if (!ip) return;
    remoteUpdateStatusByIP[ip] = { pending: true, ok: false };
    fetch(API_BASE + '/update-status?ip=' + encodeURIComponent(ip))
      .then(function (res) { return res.json(); })
      .then(function (body) {
        if (body.status !== 'success' || !body.data) {
          remoteUpdateStatusByIP[ip] = { pending: false, ok: false, err: true, can_apply: false, apply_version: '' };
          updateAllHostApplyButtons();
          return;
        }
        var d = body.data;
        remoteUpdateStatusByIP[ip] = {
          pending: false,
          ok: true,
          can_apply: !!d.can_apply,
          apply_version: d.apply_version || '',
          remote_version: d.remote_current_version || ''
        };
        updateAllHostApplyButtons();
      })
      .catch(function () {
        remoteUpdateStatusByIP[ip] = { pending: false, ok: false, err: true, can_apply: false, apply_version: '' };
        updateAllHostApplyButtons();
      });
  }

  function fetchUpdateStatusForAllRemoteHosts() {
    var cards = document.querySelectorAll('#discovered-hosts .host-card:not(.self-card)');
    var seen = {};
    for (var i = 0; i < cards.length; i++) {
      var hip = cards[i].getAttribute('data-host-ip');
      if (hip && !seen[hip]) {
        seen[hip] = true;
        fetchUpdateStatusForRemote(hip);
      }
    }
  }

  function fetchUpdateStatus() {
    fetch(API_BASE + '/update-status')
      .then(function (res) { return res.json(); })
      .then(function (body) {
        if (body.status !== 'success' || !body.data) return;
        var d = body.data;
        lastUpdateStatus = {
          can_apply: !!d.can_apply,
          apply_version: d.apply_version || '',
          staging_versions: d.staging_versions || [],
          remove_version: d.remove_version || '',
          staging_dual_agents: !!d.staging_dual_agents
        };
        var variantFs = el('agent-variant-fieldset');
        if (variantFs) {
          variantFs.hidden = !lastUpdateStatus.staging_dual_agents;
          if (!variantFs.hidden) applyLocalVariantDefault();
        }
        var applyBtn = el('apply-update-btn');
        var removeBtn = el('remove-upload-btn');
        var stagingDisplay = el('staging-version-display');
        if (applyBtn) applyBtn.disabled = !lastUpdateStatus.can_apply;
        if (removeBtn) removeBtn.disabled = !(lastUpdateStatus.staging_versions && lastUpdateStatus.staging_versions.length > 0);
        if (stagingDisplay) {
          stagingDisplay.textContent = lastUpdateStatus.staging_versions && lastUpdateStatus.staging_versions.length > 0
            ? '스테이징: ' + lastUpdateStatus.staging_versions.join(', ')
            : '';
        }
        updateReuseConfigVisibility();
        updateAllHostApplyButtons();
        fetchUpdateStatusForAllRemoteHosts();
        refreshRemoteBulkButtonsState();
      })
      .catch(function () {});
  }

  function hasUploadableSelection() {
    var bundle = el('upload-bundle');
    return !!(bundle && bundle.files && bundle.files[0]);
  }

  function updateUploadButtonState() {
    var uploadBtn = el('upload-btn');
    if (!uploadBtn) return;
    uploadBtn.disabled = !hasUploadableSelection();
    updateAllHostApplyButtons();
  }

  function resetUploadForm() {
    var bundleInput = el('upload-bundle');
    if (bundleInput) { bundleInput.value = ''; }
    var uploadBtn = el('upload-btn');
    if (uploadBtn) uploadBtn.disabled = true;
    updateAllHostApplyButtons();
  }

  function doUpload() {
    var bundleInput = el('upload-bundle');
    var status = el('upload-status');
    var applyBtn = el('apply-update-btn');
    if (!bundleInput || !bundleInput.files[0]) {
      status.textContent = 'tar.gz 번들 파일을 선택하세요.';
      return;
    }
    var formData = new FormData();
    formData.append('bundle', bundleInput.files[0]);
    status.textContent = '업로드 중(번들 검증)…';
    fetch(API_BASE + '/upload', {
      method: 'POST',
      body: formData
    })
      .then(function (res) { return res.json(); })
      .then(function (body) {
        if (body.status === 'success' && body.data && body.data.version) {
          lastUploadedVersion = body.data.version;
          status.textContent = '버전 ' + body.data.version + ' 스테이징에 업로드됨. 같은 버전으로 원격 적용 가능.';
          fetchUpdateStatus();
          updateAllHostApplyButtons();
        } else {
          status.textContent = body.data || '업로드 실패.';
        }
      })
      .catch(function () {
        status.textContent = '업로드 요청 실패.';
      });
  }

  function doApplyUpdate() {
    var version = lastUpdateStatus.apply_version;
    if (!version || !lastUpdateStatus.can_apply) return;
    var status = el('apply-update-status');
    var applyBtn = el('apply-update-btn');
    var selfCard = el('self-info') && el('self-info').querySelector('.host-card');
    var reusePreviousConfig = getReusePreviousConfig();
    var reuseCheckboxVisible = isLocalReuseConfigVisible();

    confirmApplyConfigChoice(reusePreviousConfig, reuseCheckboxVisible, function () {
      if (applyBtn) applyBtn.disabled = true;
      if (status) status.textContent = '업데이트 적용 요청 중…';
      showCardUpdating(selfCard, true);
      fetch(API_BASE + '/apply-update', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          version: version,
          agent_variant: getSelectedAgentVariant(),
          reuse_previous_config: reusePreviousConfig
        })
      })
        .then(function (res) { return res.json(); })
        .then(function (body) {
          if (body.status === 'success') {
            fetchUpdateStatus();
            if (status) status.textContent = '업데이트 적용 중… 재시작 후 정보를 자동으로 불러옵니다.';
            startUpdateLogPolling(version, selfCard, '');
            pollUntilHostJsonOk(API_BASE + '/self', 15, 4000, 2000, function (body2) {
              if (selfCard) updateHostCardDetails(selfCard, body2.data);
              refreshAllPanelsAfterUpdate(selfCard, '');
              updateAllHostApplyButtons();
              if (selfCard) showCardUpdating(selfCard, false);
              if (status) status.textContent = '적용 완료. 업데이트 기록·config·버전·상태를 반영했습니다.';
              if (applyBtn) fetchUpdateStatus();
            }, function (networkFailure) {
              if (selfCard) refreshAllPanelsAfterUpdate(selfCard, '');
              showCardUpdating(selfCard, false);
              if (status) {
                status.textContent = networkFailure
                  ? '연결 실패. 페이지를 새로고침해 보세요.'
                  : '서버 응답이 지연됩니다. 잠시 후 새로고침하세요.';
              }
              if (applyBtn) fetchUpdateStatus();
            });
          } else {
            if (status) status.textContent = body.data || '적용 실패.';
            showCardUpdating(selfCard, false);
            fetchUpdateStatus();
          }
        })
        .catch(function () {
          if (status) status.textContent = '요청 실패. 서버가 재시작 중일 수 있습니다. 잠시 후 페이지를 새로고침해 보세요.';
          showCardUpdating(selfCard, false);
          fetchUpdateStatus();
        });
    });
  }

  function resolveConfigContext(cardEl, ip) {
    if (!cardEl || ip === undefined || ip === null || ip === '') {
      return {
        cardEl: document.querySelector('#self-info .host-card') || null,
        ip: '',
        editor: el('self-current-config-editor'),
        statusEl: el('self-current-config-status')
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
    var url = API_BASE + '/current-config';
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
    return fetch(API_BASE + '/current-config', {
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
    var modal = el('config-editor-modal');
    var modalEditor = el('config-editor-modal-textarea');
    var title = el('config-editor-modal-title');
    var modalStatus = el('config-editor-modal-status');
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
    var modalEditor = el('config-editor-modal-textarea');
    if (configModalContext && modalEditor) {
      var ctx = resolveConfigContext(configModalContext.cardEl, configModalContext.ip);
      if (ctx.editor) ctx.editor.value = modalEditor.value;
    }
    var modal = el('config-editor-modal');
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
    fetchCurrentConfigForContext(ctx, el('config-editor-modal-textarea'), el('config-editor-modal-status'))
      .then(function () {
        var modalStatus = el('config-editor-modal-status');
        if (ctx.statusEl && modalStatus) ctx.statusEl.textContent = modalStatus.textContent;
      });
  }

  function saveConfigEditorModal() {
    if (!configModalContext) return;
    var ctx = resolveConfigContext(configModalContext.cardEl, configModalContext.ip);
    var modalEditor = el('config-editor-modal-textarea');
    var content = modalEditor ? modalEditor.value : '';
    saveCurrentConfigForContext(ctx, content, el('config-editor-modal-status'))
      .then(function () {
        var modalStatus = el('config-editor-modal-status');
        if (ctx.statusEl && modalStatus) ctx.statusEl.textContent = modalStatus.textContent;
      });
  }

  function initConfigEditorModal() {
    var modal = el('config-editor-modal');
    if (!modal || modal.getAttribute('data-bound')) return;
    modal.setAttribute('data-bound', '1');
    var backdrop = modal.querySelector('.config-editor-modal-backdrop');
    var closeBtn = el('config-editor-modal-close-btn');
    var dismissBtn = el('config-editor-modal-dismiss-btn');
    var loadBtn = el('config-editor-modal-load-btn');
    var saveBtn = el('config-editor-modal-save-btn');
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
    var pre = el('self-update-log-output');
    var warningEl = el('self-update-rollback-warning');
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
    var container = el('self-versions-list-container');
    var statusEl = el('self-versions-status');
    var removeBtn = el('self-versions-remove-btn');
    if (!container) return Promise.resolve();
    container.innerHTML = '<div class="versions-loading">불러오는 중…</div>';
    if (statusEl) statusEl.textContent = '';
    if (removeBtn) removeBtn.disabled = true;
    return fetch(API_BASE + '/versions/list')
      .then(function (res) { return res.json(); })
      .then(function (body) {
        if (body.status !== 'success' || !body.data || !body.data.versions) {
          container.innerHTML = '<div class="versions-loading">목록을 불러올 수 없습니다.</div>';
          return;
        }
        var versions = body.data.versions;
        if (versions.length === 0) {
          container.innerHTML = '<div class="versions-loading">설치된 버전이 없습니다.</div>';
          fillVersionsSwitchSelect(el('self-versions-switch-select'), []);
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
        var s = el('self-versions-switch-select');
        setVersionsSwitchHint(null, s && s.value ? s.value : '');
      });
  }

  function syncVersionsRemoveButton(removeBtn, listContainer) {
    if (!removeBtn || !listContainer) return;
    var checked = listContainer.querySelectorAll('.versions-list-wrapper .versions-list input[type="checkbox"]:not(:disabled):checked');
    removeBtn.disabled = checked.length === 0;
  }

  function updateVersionsRemoveButtonState() {
    syncVersionsRemoveButton(el('self-versions-remove-btn'), el('self-versions-list-container'));
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
    var switchSel = cardEl ? cardEl.querySelector('.card-versions-switch-select') : el('self-versions-switch-select');
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
    var hint = cardEl ? cardEl.querySelector('.versions-switch-hint') : el('self-versions-switch-hint');
    if (!hint) return;
    hint.textContent = versionKey ? ('버전 ' + versionKey + ' 을(를) 선택했습니다.') : '';
  }

  function updateVersionsSwitchButtonFromSelect(cardEl) {
    var sel = cardEl ? cardEl.querySelector('.card-versions-switch-select') : el('self-versions-switch-select');
    var btn = cardEl ? cardEl.querySelector('.card-versions-switch-btn') : el('self-versions-switch-btn');
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
    return fetch(API_BASE + '/versions/list?ip=' + encodeURIComponent(ip))
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
    fetch(API_BASE + '/versions/remove', {
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
    var sel = cardEl ? cardEl.querySelector('.card-versions-switch-select') : el('self-versions-switch-select');
    var statusEl = cardEl ? cardEl.querySelector('.card-versions-status') : el('self-versions-status');
    var btn = cardEl ? cardEl.querySelector('.card-versions-switch-btn') : el('self-versions-switch-btn');
    if (!sel || !btn || btn.disabled) return;
    var version = sel.value;
    if (!version) return;
    var payload = { version: version };
    if (ip) payload.ip = ip;
    if (statusEl) statusEl.textContent = '전환 적용 중…';
    btn.disabled = true;
    fetch(API_BASE + '/versions/switch-current', {
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
          scheduleRefreshAfterSwitchCurrent(cardEl, ip, version, statusEl);
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
    var container = el('self-versions-list-container');
    var statusEl = el('self-versions-status');
    var removeBtn = el('self-versions-remove-btn');
    if (!container || !removeBtn || removeBtn.disabled) return;
    var checked = container.querySelectorAll('.versions-list-wrapper .versions-list input[type="checkbox"]:not(:disabled):checked');
    if (checked.length === 0) return;
    var versions = [];
    for (var i = 0; i < checked.length; i++) {
      versions.push(checked[i].getAttribute('data-version'));
    }
    if (statusEl) statusEl.textContent = '삭제 중…';
    removeBtn.disabled = true;
    fetch(API_BASE + '/versions/remove', {
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

  el('discovery-btn').addEventListener('click', runDiscovery);
  var pushConfigAllBtn = el('push-config-all-remotes-btn');
  if (pushConfigAllBtn) {
    pushConfigAllBtn.addEventListener('click', runPushConfigToAllRemotes);
  }
  var restartAllBtn = el('restart-all-remotes-btn');
  if (restartAllBtn) {
    restartAllBtn.addEventListener('click', runRestartAllRemotes);
  }
  var applyUpdateAllBtn = el('apply-update-all-remotes-btn');
  if (applyUpdateAllBtn) {
    applyUpdateAllBtn.addEventListener('click', runApplyUpdateAllRemotes);
  }
  var rollbackAllBtn = el('rollback-all-remotes-btn');
  if (rollbackAllBtn) {
    rollbackAllBtn.addEventListener('click', runRollbackAllRemotes);
  }
  el('upload-btn').addEventListener('click', doUpload);
  el('apply-update-btn').addEventListener('click', doApplyUpdate);
  el('reset-selection-btn').addEventListener('click', resetUploadForm);
  el('remove-upload-btn').addEventListener('click', doRemoveUpload);
  el('upload-bundle').addEventListener('change', updateUploadButtonState);

  resetUploadForm();
  fetchUpdateStatus();
  updateAllHostApplyButtons();
  initConfigEditorModal();
  initBulkResultsModal();
  initBulkRemoteStatusDismiss();
  loadSelf();

  document.addEventListener('visibilitychange', function () {
    if (document.hidden) {
      Object.keys(remoteHealthState).forEach(function (ip) {
        var st = remoteHealthState[ip];
        if (st && st.timerId != null) {
          clearTimeout(st.timerId);
          st.timerId = null;
        }
      });
    } else {
      Object.keys(remoteHealthState).forEach(function (ip) {
        scheduleRemoteHealthTick(ip);
      });
    }
  });
  setTimeout(function () { enumerateDiscoveredRemoteHealth(); }, 0);
  refreshRemoteBulkButtonsState();
})();
