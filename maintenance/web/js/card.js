/* eslint-disable */
(function (M) {
'use strict';
  const serverIconSvg = '<svg class="host-icon" xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="currentColor" aria-hidden="true"><rect x="2" y="4" width="20" height="4" rx="1"/><rect x="2" y="10" width="20" height="4" rx="1"/><rect x="2" y="16" width="20" height="4" rx="1"/><circle cx="6" cy="6" r="0.8"/><circle cx="6" cy="12" r="0.8"/><circle cx="6" cy="18" r="0.8"/></svg>';

  function renderHostRow(host, isSelf) {
    var row = document.createElement('div');
    row.className = 'host-row' + (isSelf ? ' host-row--local' : '');
    if (host.host_ip) row.setAttribute('data-host-ip', host.host_ip);
    var label = M.getHostRowLabel(host, isSelf);
    var card = renderHostCard(host, isSelf);
    row.innerHTML =
      '<div class="host-row__header" role="button" tabindex="0" aria-expanded="false">' +
      '<span class="host-row__dot" aria-hidden="true"></span>' +
      '<span class="host-row__label">' + M.escapeHtml(label) + '</span>' +
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
            M.fetchUpdateLogForCard(card, ip);
            M.fetchCurrentConfigForCard(card, ip);
            M.fetchVersionsListForCard(card, ip);
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
    if (labelEl) labelEl.textContent = M.getHostRowLabel(host || {}, isSelf);
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
    div.setAttribute('data-build-variant', M.defaultAgentVariantFromBuild(host.build_variant));
    var serviceActionsHtml =
      '<button type="button" class="service-btn status-refresh-btn">상태 새로고침</button>' +
      '<button type="button" class="service-btn service-restart-btn">서비스 재시작</button>';
    var remoteUpdateBarHtml = isSelf ? '' :
      '<div class="card-remote-update-bar">' +
      '<div class="card-update-apply-options">' +
      M.cardReuseConfigHtml() +
      M.cardVariantRadiosHtml(host.host_ip || '', host.build_variant) +
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
      '<dt>CPU UUID</dt><dd>' + M.escapeHtml(host.cpu_uuid || '-') + '</dd>' +
      '<dt>버전</dt><dd>' + M.escapeHtml(host.version || '-') + (host.build_variant ? ' <span class="build-variant-badge">(' + M.escapeHtml(host.build_variant) + ')</span>' : '') + '</dd>' +
      '<dt>IP</dt><dd>' + M.escapeHtml(ipsDisplay) + '</dd>' +
      '<dt>응답한 IP</dt><dd>' + M.escapeHtml(respondedFromDisplay) + '</dd>' +
      '<dt>호스트명</dt><dd>' + M.escapeHtml(host.hostname || '-') + '</dd>' +
      '<dt>서비스 포트</dt><dd>' + (host.service_port != null ? host.service_port : '-') + '</dd>' +
      '<dt>CPU</dt><dd>' + M.escapeHtml(host.cpu_info || '-') + (host.cpu_usage_percent != null ? ' (' + host.cpu_usage_percent.toFixed(1) + '%)' : '') + '</dd>' +
      '<dt>메모리</dt><dd>' + M.formatMemory(host) + '</dd>' +
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
        var url = isSelf ? (M.API_BASE + '/self') : (M.API_BASE + '/host-info?ip=' + encodeURIComponent(ip));
        fetch(url)
          .then(function (res) { return res.json(); })
          .then(function (body) {
            if (body.status === 'success' && body.data) {
              M.updateHostCardDetails(cardEl, body.data);
              if (!isSelf) {
                M.fetchUpdateStatusForRemote(ip);
                M.updateAllHostApplyButtons();
              }
            } else {
              if (summary) summary.textContent = isSelf ? (body.data || '내 정보를 불러올 수 없습니다.') : (body.data || '호스트 정보를 불러올 수 없습니다.');
            }
            M.fetchServiceStatus(cardEl, isSelf ? '' : ip);
          })
          .catch(function () {
            if (summary) summary.textContent = isSelf ? '내 정보 요청 실패.' : '호스트 정보 요청 실패.';
            M.fetchServiceStatus(cardEl, isSelf ? '' : ip);
          });
      });
    }
    var restartBtn = cardEl.querySelector('.service-restart-btn');
    if (restartBtn) {
      restartBtn.addEventListener('click', function () {
        if (!isSelf && !M.isRemoteHostReachableForControl(cardEl)) {
          if (summary) summary.textContent = M.remoteHostUnreachableReason(cardEl);
          return;
        }
        if (summary) summary.textContent = '재시작 중…';
        var restartIp = isSelf ? 'self' : ip;
        function afterRestartMaybeRefresh() {
          if (summary) summary.textContent = '재시작되었습니다. 잠시 후 상태를 불러옵니다.';
          var delay = isSelf ? 2000 : 3500;
          var targetIp = isSelf ? '' : ip;
          setTimeout(function () {
            M.refreshHostCardDetails(cardEl, targetIp);
            M.fetchServiceStatus(cardEl, targetIp);
          }, delay);
        }
        function isRestartInProgressError(msg) {
          if (!msg || typeof msg !== 'string') return false;
          var s = msg.toLowerCase();
          return /terminated|connection reset|원격 재시작 요청 실패|eof/.test(s);
        }
        fetch(M.API_BASE + '/service-control', {
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
                if (isSelf) M.fetchServiceStatus(cardEl, '');
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
      if (logRefreshBtn) logRefreshBtn.addEventListener('click', function () { M.fetchUpdateLogForCard(cardEl, ip); });
      var configLoadBtn = cardEl.querySelector('.card-current-config-load-btn');
      var configSaveBtn = cardEl.querySelector('.card-current-config-save-btn');
      var configExpandBtn = cardEl.querySelector('.card-current-config-expand-btn');
      if (configLoadBtn) configLoadBtn.addEventListener('click', function () { M.fetchCurrentConfigForCard(cardEl, ip); });
      if (configSaveBtn) configSaveBtn.addEventListener('click', function () { M.saveCurrentConfigForCard(cardEl, ip); });
      if (configExpandBtn) configExpandBtn.addEventListener('click', function () { M.openConfigEditorModal(cardEl, ip); });
      var versionsRefreshBtn = cardEl.querySelector('.card-versions-list-refresh-btn');
      var versionsRemoveBtn = cardEl.querySelector('.card-versions-remove-btn');
      if (versionsRefreshBtn) versionsRefreshBtn.addEventListener('click', function () { M.fetchVersionsListForCard(cardEl, ip); });
      if (versionsRemoveBtn) versionsRemoveBtn.addEventListener('click', function () { M.doVersionsRemoveForCard(cardEl, ip); });
      var swSel = cardEl.querySelector('.card-versions-switch-select');
      var swBtn = cardEl.querySelector('.card-versions-switch-btn');
      if (swSel) {
        swSel.addEventListener('change', function () {
          M.updateVersionsSwitchButtonFromSelect(cardEl);
          M.setVersionsSwitchHint(cardEl, swSel.value);
        });
      }
      if (swBtn) swBtn.addEventListener('click', function () { M.doVersionsSwitch(cardEl, ip); });
    }
    if (isSelf) return;
    function doControl(action) {
      if (summary) summary.textContent = '갱신 중…';
      fetch(M.API_BASE + '/service-control', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ ip: ip, action: action })
      })
        .then(function (res) { return res.json(); })
        .then(function (body) {
          if (body.status === 'success') {
            M.fetchServiceStatus(cardEl, ip);
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
        if (!M.isRemoteHostReachableForControl(card)) {
          if (summary) summary.textContent = M.remoteHostUnreachableReason(card);
          return;
        }
        var hostVersion = card ? (card.getAttribute('data-host-version') || '') : '';
        var stPre = M.remoteUpdateStatusByIP[ip];
        var canProceed;
        if (stPre && stPre.ok) {
          canProceed = !!(stPre.can_apply && stPre.apply_version);
        } else if (M.hasUploadableSelection()) {
          canProceed = true;
        } else {
          canProceed = M.canApplyToThisRemoteHostLegacy(hostVersion);
        }
        if (!canProceed) {
          if (summary) summary.textContent = '이 호스트에 적용할 스테이징 버전이 없거나 이미 동일 버전입니다.';
          return;
        }

        var reusePreviousConfig = M.getCardReusePreviousConfig(card);
        var reuseCheckboxVisible = M.isCardReuseConfigVisible(card);

        M.confirmApplyConfigChoice(reusePreviousConfig, reuseCheckboxVisible, function () {
          applyHostBtn.disabled = true;
          if (summary) summary.textContent = '업데이트 적용 중…';
          if (card) M.toggleCardVariantSelector(card, false);

          function recheckApplyButton() {
            var c = applyHostBtn.closest && applyHostBtn.closest('.host-card');
            var hip = c ? (c.getAttribute('data-host-ip') || '') : '';
            if (hip) {
              M.fetchUpdateStatusForRemote(hip);
            } else {
              M.updateAllHostApplyButtons();
            }
          }

          function doApplyToHost(version) {
            M.showCardUpdating(cardEl, true);
            fetch(M.API_BASE + '/apply-update', {
              method: 'POST',
              headers: { 'Content-Type': 'application/json' },
              body: JSON.stringify({
                version: version,
                ip: ip,
                agent_variant: M.getCardAgentVariant(cardEl),
                reuse_previous_config: reusePreviousConfig
              })
            })
              .then(function (res) { return res.json(); })
              .then(function (body) {
                if (body.status === 'success') {
                  M.scheduleRefreshAfterApply(cardEl, ip, summary, body.data, version);
                } else {
                  updateStatusUI(cardEl, null, body.data || '적용 실패');
                  M.showCardUpdating(cardEl, false);
                }
              })
              .catch(function () {
                updateStatusUI(cardEl, null, '요청 실패');
                M.showCardUpdating(cardEl, false);
              })
              .finally(recheckApplyButton);
          }

          var stV = M.remoteUpdateStatusByIP[ip];
          var applicableVersion = (stV && stV.ok && stV.apply_version) ? stV.apply_version : M.getApplicableVersion();
          if (applicableVersion) {
            doApplyToHost(applicableVersion);
            return;
          }
          // tar.gz 번들 선택 시: 로컬 스테이징 없이 원격으로만 전송 (multipart apply-update)
          var bundleInput = M.el('upload-bundle');
          if (!bundleInput || !bundleInput.files[0]) {
            if (summary) summary.textContent = '원격 적용할 tar.gz 번들을 선택하세요.';
            applyHostBtn.disabled = false;
            recheckApplyButton();
            return;
          }
          var formData = new FormData();
          formData.append('ip', ip);
          formData.append('bundle', bundleInput.files[0]);
          formData.append('agent_variant', M.getCardAgentVariant(cardEl));
          formData.append('reuse_previous_config', reusePreviousConfig ? 'true' : 'false');
          M.showCardUpdating(cardEl, true);
          fetch(M.API_BASE + '/apply-update', {
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
                M.scheduleRefreshAfterApply(cardEl, ip, summary, body.data, ver);
              } else {
                updateStatusUI(cardEl, null, body.data || '적용 실패');
                M.showCardUpdating(cardEl, false);
              }
            })
            .catch(function () {
              updateStatusUI(cardEl, null, '요청 실패');
              M.showCardUpdating(cardEl, false);
            })
            .finally(recheckApplyButton);
        });
      });
    }
    M.bindRemoteHealthForCard(cardEl);
  }

  function loadSelf() {
    const container = M.el('self-info');
    container.innerHTML = '<div class="host-loading">불러오는 중…</div>';
    fetch(M.API_BASE + '/self')
      .then(function (res) { return res.json(); })
      .then(function (body) {
        try {
          if (body.status === 'success' && body.data) {
            container.innerHTML = '';
            var row = renderHostRow(body.data, true);
            container.appendChild(row);
            var card = row.querySelector('.host-card');
            bindServiceControlButtons(card);
            M.fetchServiceStatus(card, '');
            var logRefreshBtn = M.el('self-update-log-refresh-btn');
            var versionsRefreshBtn = M.el('self-versions-list-refresh-btn');
            var versionsRemoveBtn = M.el('self-versions-remove-btn');
            if (logRefreshBtn) logRefreshBtn.addEventListener('click', function () { M.fetchUpdateLog(); });
            if (versionsRefreshBtn) versionsRefreshBtn.addEventListener('click', M.fetchVersionsList);
            if (versionsRemoveBtn) versionsRemoveBtn.addEventListener('click', M.doVersionsRemove);
            var swSelSelf = M.el('self-versions-switch-select');
            var swBtnSelf = M.el('self-versions-switch-btn');
            if (swSelSelf) {
              swSelSelf.addEventListener('change', function () {
                M.updateVersionsSwitchButtonFromSelect(null);
                M.setVersionsSwitchHint(null, swSelSelf.value);
              });
            }
            if (swBtnSelf) swBtnSelf.addEventListener('click', function () { M.doVersionsSwitch(null, ''); });
            var configLoadBtn = M.el('self-current-config-load-btn');
            var configSaveBtn = M.el('self-current-config-save-btn');
            var configExpandBtn = M.el('self-current-config-expand-btn');
            if (configLoadBtn) configLoadBtn.addEventListener('click', M.fetchCurrentConfig);
            if (configSaveBtn) configSaveBtn.addEventListener('click', M.saveCurrentConfig);
            if (configExpandBtn) configExpandBtn.addEventListener('click', function () { M.openConfigEditorModal(null, ''); });
            M.fetchUpdateLog();
            M.fetchCurrentConfig();
            M.fetchVersionsList();
          } else {
            container.innerHTML = '<div class="host-error">내 정보를 불러올 수 없습니다.</div>';
          }
        } catch (err) {
          console.error('loadSelf render failed:', err);
          container.innerHTML = '<div class="host-error">내 정보를 불러올 수 없습니다.</div>';
        }
      })
      .catch(function () {
        container.innerHTML = '<div class="host-error">내 정보를 불러올 수 없습니다.</div>';
      });
  }

  // exports
  M.bindHostRowToggle = bindHostRowToggle;
  M.bindServiceControlButtons = bindServiceControlButtons;
  M.bindStatusToggle = bindStatusToggle;
  M.loadSelf = loadSelf;
  M.parseActiveFromOutput = parseActiveFromOutput;
  M.renderHostCard = renderHostCard;
  M.renderHostRow = renderHostRow;
  M.updateHostRowDot = updateHostRowDot;
  M.updateHostRowLabel = updateHostRowLabel;
  M.updateStatusUI = updateStatusUI;
})(window.MolMaintenance = window.MolMaintenance || {});
