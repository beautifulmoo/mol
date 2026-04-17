(function () {
  var _api = (typeof window !== 'undefined' && window.__CONTRABASS_API_PREFIX__) || '/api/v1';
  if (typeof _api === 'string' && _api.length > 1 && _api.charAt(_api.length - 1) === '/') {
    _api = _api.slice(0, -1);
  }
  var API_BASE = _api;

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
    if (row) row.classList.toggle('host-row--remote-health-dead', !!dead);
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
    var statusRowHtml = '<div class="self-status-row">' +
      '<div class="service-status-block">' +
      '<div class="service-status-header-row">' +
      '<div class="service-status-header" role="button" tabindex="0" aria-expanded="false">' +
      '<span class="service-status-icon" aria-hidden="true">▶</span> ' +
      '<span class="service-status-summary">불러오는 중…</span>' +
      '</div>' +
      '<div class="service-status-buttons">' +
      '<button type="button" class="service-btn status-refresh-btn">상태 새로고침</button>' +
      '<button type="button" class="service-btn service-restart-btn">서비스 재시작</button>' +
      (isSelf ? '' : '<button type="button" class="service-btn service-start apply-update-host" disabled>업데이트 적용</button>') +
      '</div></div>' +
      '<pre class="service-status-output"></pre>' +
      '</div></div>';
    var rightColumnSelf = '<div class="self-card-right-column">' +
      '<div class="card-right-log">' +
      '<h4 class="card-right-title">업데이트 기록 (최근 10건)</h4>' +
      '<div id="self-update-rollback-warning" class="update-rollback-warning" role="alert" aria-live="polite" hidden></div>' +
      '<button type="button" id="self-update-log-refresh-btn" class="service-btn">목록 새로고침</button>' +
      '<pre id="self-update-log-output" class="update-log-output card-right-log-output">(새로고침으로 로그 불러오기)</pre>' +
      '</div>' +
      '<div class="card-right-config">' +
      '<h4 class="card-right-title">config.yaml (current)</h4>' +
      '<textarea id="self-current-config-editor" class="config-editor card-right-config-editor" placeholder="불러오기로 current 버전의 config.yaml을 불러옵니다." spellcheck="false"></textarea>' +
      '<div class="card-right-config-actions">' +
      '<button type="button" id="self-current-config-load-btn" class="service-btn">불러오기</button>' +
      '<button type="button" id="self-current-config-save-btn" class="service-btn">저장</button>' +
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
      '<button type="button" class="service-btn card-update-log-refresh-btn">목록 새로고침</button>' +
      '<pre class="update-log-output card-right-log-output">(새로고침으로 로그 불러오기)</pre>' +
      '</div>' +
      '<div class="card-right-config">' +
      '<h4 class="card-right-title">config.yaml (current)</h4>' +
      '<textarea class="config-editor card-right-config-editor" placeholder="불러오기로 current 버전의 config.yaml을 불러옵니다." spellcheck="false"></textarea>' +
      '<div class="card-right-config-actions">' +
      '<button type="button" class="service-btn card-current-config-load-btn">불러오기</button>' +
      '<button type="button" class="service-btn card-current-config-save-btn">저장</button>' +
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
      '<dt>버전</dt><dd>' + escapeHtml(host.version || '-') + '</dd>' +
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
      var startBtn = cardEl.querySelector('.service-start, .host-control-start');
      var stopBtn = cardEl.querySelector('.service-stop');
      var active = parseActiveFromOutput(output);
      if (startBtn) startBtn.disabled = active;
      if (stopBtn) stopBtn.disabled = !active;
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
              if (!isSelf) updateAllHostApplyButtons();
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
      if (configLoadBtn) configLoadBtn.addEventListener('click', function () { fetchCurrentConfigForCard(cardEl, ip); });
      if (configSaveBtn) configSaveBtn.addEventListener('click', function () { saveCurrentConfigForCard(cardEl, ip); });
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
    var startBtn = cardEl.querySelector('.service-start, .host-control-start');
    var stopBtn = cardEl.querySelector('.service-stop');
    if (startBtn) startBtn.addEventListener('click', function () { doControl('start'); });
    if (stopBtn) stopBtn.addEventListener('click', function () { doControl('stop'); });

    var applyHostBtn = cardEl.querySelector('.apply-update-host');
    if (applyHostBtn) {
      applyHostBtn.addEventListener('click', function () {
        var card = applyHostBtn.closest && applyHostBtn.closest('.host-card');
        var hostVersion = card ? (card.getAttribute('data-host-version') || '') : '';
        if (!canApplyToThisRemoteHost(hostVersion)) {
          if (summary) summary.textContent = '이 호스트에 적용할 스테이징 버전이 없거나 이미 동일 버전입니다.';
          return;
        }
        applyHostBtn.disabled = true;
        if (summary) summary.textContent = '업데이트 적용 중…';

        function recheckApplyButton() {
          var c = applyHostBtn.closest && applyHostBtn.closest('.host-card');
          var hv = c ? (c.getAttribute('data-host-version') || '') : '';
          applyHostBtn.disabled = !canApplyToThisRemoteHost(hv);
        }

        function doApplyToHost(version) {
          showCardUpdating(cardEl, true);
          fetch(API_BASE + '/apply-update', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ version: version, ip: ip })
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

        var applicableVersion = getApplicableVersion();
        if (applicableVersion) {
          doApplyToHost(applicableVersion);
          return;
        }
        // tar.gz 번들 선택 시: 로컬 스테이징 없이 원격으로만 전송 (multipart apply-update)
        var bundleInput = el('upload-bundle');
        if (!bundleInput || !bundleInput.files[0]) {
          if (summary) summary.textContent = '원격 적용할 tar.gz 번들을 선택하세요.';
          recheckApplyButton();
          return;
        }
        var formData = new FormData();
        formData.append('ip', ip);
        formData.append('bundle', bundleInput.files[0]);
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
                var m = body.data.match(/버전\s+(\S+)\s+적용 완료/);
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
    }
    bindRemoteHealthForCard(cardEl);
  }

  function getApplicableVersion() {
    if (lastUpdateStatus.staging_versions && lastUpdateStatus.staging_versions.length > 0) {
      return lastUpdateStatus.staging_versions[0];
    }
    return lastUploadedVersion || '';
  }

  function canApplyToThisRemoteHost(hostVersion) {
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

  /** After apply-update: reload update log, current config, versions list, service status (and local staging state). */
  function refreshAllPanelsAfterUpdate(cardEl, ip) {
    if (ip) {
      if (!cardEl) return;
      fetchUpdateLogForCard(cardEl, ip);
      fetchCurrentConfigForCard(cardEl, ip);
      fetchVersionsListForCard(cardEl, ip);
      fetchServiceStatus(cardEl, ip);
    } else {
      fetchUpdateLog();
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
      if (dds && dds.length >= 8) dds[1].textContent = appliedVersion;
      updateAllHostApplyButtons();
    }
    var url = API_BASE + '/host-info?ip=' + encodeURIComponent(ip);
    pollUntilHostJsonOk(url, 8, 5000, 2000, function (body) {
      updateHostCardDetails(cardEl, body.data);
      updateAllHostApplyButtons();
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
      var logPoll = setInterval(function () { fetchUpdateLog(true); }, 2000);
      function finishPoll() {
        clearInterval(logPoll);
      }
      fetchUpdateStatus();
      pollUntilHostJsonOk(API_BASE + '/self', 15, 4000, 2000, function (body2) {
        finishPoll();
        if (selfCard) updateHostCardDetails(selfCard, body2.data);
        refreshAllPanelsAfterUpdate(selfCard, '');
        updateAllHostApplyButtons();
        if (selfCard) showCardUpdating(selfCard, false);
        if (statusEl) statusEl.textContent = '전환 완료. 업데이트 기록·config·버전·상태를 반영했습니다.';
        fetchUpdateStatus();
        updateVersionsSwitchButtonFromSelect(null);
      }, function (networkFailure) {
        finishPoll();
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
    var applicableVersion = getApplicableVersion();
    var btns = document.querySelectorAll('.apply-update-host');
    for (var i = 0; i < btns.length; i++) {
      var btn = btns[i];
      var card = btn.closest && btn.closest('.host-card');
      var hostVersion = card ? (card.getAttribute('data-host-version') || '') : '';
      var canApply = canApplyToThisRemoteHost(hostVersion);
      btn.disabled = !canApply;
      btn.title = getApplyButtonTitle(hostVersion, canApply, applicableVersion);
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

  function applyUpdateLogResponse(pre, warningEl, body) {
    if (body.status === 'success' && body.data) {
      pre.textContent = body.data.output !== undefined ? body.data.output : '(비어 있음)';
      if (warningEl && body.data.recent_rollback) {
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
      dds[1].textContent = host.version || '-';
      dds[2].textContent = ipDisplay;
      dds[3].textContent = respondedFromDisplay;
      dds[4].textContent = host.hostname || '-';
      dds[5].textContent = host.service_port != null ? host.service_port : '-';
      dds[6].innerHTML = escapeHtml(host.cpu_info || '-') + (host.cpu_usage_percent != null ? ' (' + host.cpu_usage_percent.toFixed(1) + '%)' : '');
      dds[7].textContent = formatMemory(host);
    }
    var row = cardEl.closest && cardEl.closest('.host-row');
    if (row) updateHostRowLabel(row, host, cardEl.classList.contains('self-card'));
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
          if (configLoadBtn) configLoadBtn.addEventListener('click', fetchCurrentConfig);
          if (configSaveBtn) configSaveBtn.addEventListener('click', saveCurrentConfig);
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

  function runDiscovery() {
    const btn = el('discovery-btn');
    const status = el('discovery-status');
    const list = el('discovered-hosts');
    if (!list) return;
    btn.disabled = true;
    status.textContent = 'Discovery 진행 중… (기존 호스트는 그대로 제어 가능)';
    var count = list.querySelectorAll('.host-card:not(.self-card)').length;
    var discoveryFailHandled = false;
    var evtSource = new EventSource(API_BASE + '/discovery/stream');
    evtSource.addEventListener('discoveryfail', function (e) {
      discoveryFailHandled = true;
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
          updateAllHostApplyButtons();
          registerRemoteHealthMonitoring(existing);
        } else {
          var row = renderHostRow(host, false);
          list.appendChild(row);
          var card = row.querySelector('.host-card');
          bindServiceControlButtons(card);
          fetchServiceStatus(card, ip);
          registerRemoteHealthMonitoring(card);
        }
        count = list.querySelectorAll('.host-card:not(.self-card)').length;
        status.textContent = 'Discovery 진행 중… (호스트 ' + count + '개, 응답 오는 대로 갱신)';
      } catch (err) {}
    };
    evtSource.addEventListener('done', function () {
      evtSource.close();
      btn.disabled = false;
      status.textContent = count ? '호스트 ' + count + '개 발견.' : 'Discovery 완료 (결과 없음).';
      updateAllHostApplyButtons();
    });
    evtSource.onerror = function () {
      evtSource.close();
      btn.disabled = false;
      if (discoveryFailHandled) {
        updateAllHostApplyButtons();
        return;
      }
      if (count === 0) {
        status.textContent = 'Discovery 요청 실패 (서버 연결 오류 또는 스트림 중단). journalctl -u contrabass-mole.service 로 서버 로그를 확인하세요.';
      } else {
        status.textContent = '호스트 ' + count + '개 발견.';
      }
      updateAllHostApplyButtons();
    };
  }

  var lastUploadedVersion = '';
  var lastUpdateStatus = { can_apply: false, apply_version: '', staging_versions: [], remove_version: '' };

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
          remove_version: d.remove_version || ''
        };
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
        updateAllHostApplyButtons();
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
    if (applyBtn) applyBtn.disabled = true;
    status.textContent = '업데이트 적용 요청 중…';
    showCardUpdating(selfCard, true);
    fetch(API_BASE + '/apply-update', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ version: version })
    })
      .then(function (res) { return res.json(); })
      .then(function (body) {
        if (body.status === 'success') {
          fetchUpdateStatus();
          if (status) status.textContent = '업데이트 적용 중… 재시작 후 정보를 자동으로 불러옵니다.';
          var logPoll = setInterval(function () { fetchUpdateLog(true); }, 2000);
          function finishPoll() {
            clearInterval(logPoll);
          }
          pollUntilHostJsonOk(API_BASE + '/self', 15, 4000, 2000, function (body2) {
            finishPoll();
            if (selfCard) updateHostCardDetails(selfCard, body2.data);
            refreshAllPanelsAfterUpdate(selfCard, '');
            updateAllHostApplyButtons();
            if (selfCard) showCardUpdating(selfCard, false);
            if (status) status.textContent = '적용 완료. 업데이트 기록·config·버전·상태를 반영했습니다.';
            if (applyBtn) fetchUpdateStatus();
          }, function (networkFailure) {
            finishPoll();
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
          status.textContent = body.data || '적용 실패.';
          showCardUpdating(selfCard, false);
          fetchUpdateStatus();
        }
      })
      .catch(function () {
        status.textContent = '요청 실패. 서버가 재시작 중일 수 있습니다. 잠시 후 페이지를 새로고침해 보세요.';
        showCardUpdating(selfCard, false);
        fetchUpdateStatus();
      });
  }

  function fetchCurrentConfig() {
    var editor = el('self-current-config-editor');
    var statusEl = el('self-current-config-status');
    if (!editor) return;
    if (statusEl) statusEl.textContent = '';
    editor.placeholder = '불러오는 중…';
    fetch(API_BASE + '/current-config')
      .then(function (res) { return res.json(); })
      .then(function (body) {
        editor.placeholder = '불러오기로 current 버전의 config.yaml을 불러옵니다.';
        if (body.status === 'success' && body.data && body.data.content !== undefined) {
          editor.value = body.data.content;
          if (statusEl) statusEl.textContent = '불러왔습니다.';
        } else {
          editor.value = '';
          if (statusEl) statusEl.textContent = body.data || '불러오기 실패.';
        }
      })
      .catch(function () {
        editor.placeholder = '불러오기로 current 버전의 config.yaml을 불러옵니다.';
        editor.value = '';
        if (statusEl) statusEl.textContent = '불러오기 실패.';
      });
  }

  function saveCurrentConfig() {
    var editor = el('self-current-config-editor');
    var statusEl = el('self-current-config-status');
    if (!editor) return;
    if (statusEl) statusEl.textContent = '저장 중…';
    fetch(API_BASE + '/current-config', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ content: editor.value })
    })
      .then(function (res) { return res.json(); })
      .then(function (body) {
        if (statusEl) {
          statusEl.textContent = body.status === 'success' ? '저장했습니다.' : (body.data || '저장 실패.');
        }
      })
      .catch(function () {
        if (statusEl) statusEl.textContent = '저장 요청 실패.';
      });
  }

  function fetchUpdateLog(silent) {
    var pre = el('self-update-log-output');
    var warningEl = el('self-update-rollback-warning');
    if (!pre) return;
    if (!silent) pre.textContent = '불러오는 중…';
    if (warningEl) warningEl.hidden = true;
    fetch(API_BASE + '/update-log')
      .then(function (res) { return res.json(); })
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
    fetch(API_BASE + '/update-log?ip=' + encodeURIComponent(ip))
      .then(function (res) { return res.json(); })
      .then(function (body) { applyUpdateLogResponse(pre, warningEl, body); })
      .catch(function () {
        pre.textContent = '로그를 불러올 수 없습니다.';
      });
  }

  function fetchCurrentConfigForCard(cardEl, ip) {
    if (!cardEl || !ip) return;
    var editor = cardEl.querySelector('.card-right-config-editor');
    var statusEl = cardEl.querySelector('.card-current-config-status');
    if (!editor) return;
    if (statusEl) statusEl.textContent = '';
    editor.placeholder = '불러오는 중…';
    fetch(API_BASE + '/current-config?ip=' + encodeURIComponent(ip))
      .then(function (res) { return res.json(); })
      .then(function (body) {
        editor.placeholder = '불러오기로 current 버전의 config.yaml을 불러옵니다.';
        if (body.status === 'success' && body.data && body.data.content !== undefined) {
          editor.value = body.data.content;
          if (statusEl) statusEl.textContent = '불러왔습니다.';
        } else {
          editor.value = '';
          if (statusEl) statusEl.textContent = body.data || '불러오기 실패.';
        }
      })
      .catch(function () {
        editor.placeholder = '불러오기로 current 버전의 config.yaml을 불러옵니다.';
        editor.value = '';
        if (statusEl) statusEl.textContent = '불러오기 실패.';
      });
  }

  function saveCurrentConfigForCard(cardEl, ip) {
    if (!cardEl || !ip) return;
    var editor = cardEl.querySelector('.card-right-config-editor');
    var statusEl = cardEl.querySelector('.card-current-config-status');
    if (!editor) return;
    if (statusEl) statusEl.textContent = '저장 중…';
    fetch(API_BASE + '/current-config', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ ip: ip, content: editor.value })
    })
      .then(function (res) { return res.json(); })
      .then(function (body) {
        if (statusEl) {
          statusEl.textContent = body.status === 'success' ? '저장했습니다.' : (body.data || '저장 실패.');
        }
      })
      .catch(function () {
        if (statusEl) statusEl.textContent = '저장 요청 실패.';
      });
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
  el('upload-btn').addEventListener('click', doUpload);
  el('apply-update-btn').addEventListener('click', doApplyUpdate);
  el('reset-selection-btn').addEventListener('click', resetUploadForm);
  el('remove-upload-btn').addEventListener('click', doRemoveUpload);
  el('upload-bundle').addEventListener('change', updateUploadButtonState);

  resetUploadForm();
  fetchUpdateStatus();
  updateAllHostApplyButtons();
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
})();
