/* eslint-disable */
(function (M) {
'use strict';

  function getApplicableVersion() {
    if (M.lastUpdateStatus.apply_version) {
      return M.lastUpdateStatus.apply_version;
    }
    if (M.lastUpdateStatus.staging_versions && M.lastUpdateStatus.staging_versions.length > 0) {
      return M.lastUpdateStatus.staging_versions[0];
    }
    return M.lastUploadedVersion || '';
  }

  /** Fallback when /update-status?ip= failed: approximate using newest staging vs card version (may disagree with server). */

  function canApplyToThisRemoteHostLegacy(hostVersion) {
    if (M.hasUploadableSelection()) return true;
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

  function refreshAllPanelsAfterUpdate(cardEl, ip) {
    if (ip) {
      if (!cardEl) return;
      if (!M.activeLogPollVersion) M.fetchUpdateLogForCard(cardEl, ip);
      M.fetchCurrentConfigForCard(cardEl, ip);
      M.fetchVersionsListForCard(cardEl, ip);
      M.fetchServiceStatus(cardEl, ip);
      M.fetchUpdateStatusForRemote(ip);
      M.fetchRollbackStatusForRemote(ip);
    } else {
      if (!M.activeLogPollVersion) M.fetchUpdateLog(true);
      M.fetchCurrentConfig();
      M.fetchVersionsList();
      if (cardEl) M.fetchServiceStatus(cardEl, '');
    }
    M.fetchUpdateStatus();
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
      M.startUpdateLogPolling(appliedVersion, cardEl, ip || '');
    }
    var url = M.API_BASE + '/host-info?ip=' + encodeURIComponent(ip);
    pollUntilHostJsonOk(url, 8, 5000, 2000, function (body) {
      M.updateHostCardDetails(cardEl, body.data);
      refreshAllPanelsAfterUpdate(cardEl, ip);
      if (summary) summary.textContent = successMessage || '적용 완료. 업데이트 기록·config·버전·상태를 반영했습니다.';
      M.showCardUpdating(cardEl, false);
      if (onDone) onDone();
    }, function () {
      refreshAllPanelsAfterUpdate(cardEl, ip);
      M.showCardUpdating(cardEl, false);
      if (onDone) onDone();
    });
  }

  /** After switch-current: same panel refresh as apply-update (log, config, versions, service, update-status). */

  function scheduleRefreshAfterSwitchCurrent(cardEl, ip, switchedVersion, statusEl) {
    if (!ip) {
      var selfCard = M.el('self-info') && M.el('self-info').querySelector('.host-card');
      if (selfCard) M.showCardUpdating(selfCard, true);
      if (statusEl) statusEl.textContent = '전환 반영 중… 재시작 후 정보를 자동으로 불러옵니다.';
      M.startUpdateLogPolling(switchedVersion, selfCard, '');
      M.fetchUpdateStatus();
      pollUntilHostJsonOk(M.API_BASE + '/self', 15, 4000, 2000, function (body2) {
        if (selfCard) M.updateHostCardDetails(selfCard, body2.data);
        refreshAllPanelsAfterUpdate(selfCard, '');
        updateAllHostApplyButtons();
        if (selfCard) M.showCardUpdating(selfCard, false);
        if (statusEl) statusEl.textContent = '전환 완료. 업데이트 기록·config·버전·상태를 반영했습니다.';
        M.fetchUpdateStatus();
        M.updateVersionsSwitchButtonFromSelect(null);
      }, function (networkFailure) {
        refreshAllPanelsAfterUpdate(selfCard, '');
        if (selfCard) M.showCardUpdating(selfCard, false);
        if (statusEl) {
          statusEl.textContent = networkFailure
            ? '연결 실패. 페이지를 새로고침해 보세요.'
            : '서버 응답이 지연됩니다. 잠시 후 정보를 새로고침하세요.';
        }
        M.fetchUpdateStatus();
        M.updateVersionsSwitchButtonFromSelect(null);
      });
      return;
    }
    if (cardEl) M.showCardUpdating(cardEl, true);
    if (statusEl) statusEl.textContent = '전환 반영 중… 잠시 후 상태를 다시 읽어옵니다.';
    scheduleRefreshAfterApply(
      cardEl,
      ip,
      statusEl,
      '전환 완료. 업데이트 기록·config·버전·상태를 반영했습니다.',
      switchedVersion,
      function () {
        M.updateVersionsSwitchButtonFromSelect(cardEl);
        M.fetchUpdateStatus();
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
      return !!(M.lastUpdateStatus.staging_dual_agents || M.hasUploadableSelection());
    }
    for (var i = 0; i < btns.length; i++) {
      var btn = btns[i];
      var card = btn.closest && btn.closest('.host-card');
      if (!card) continue;
      if (!card.classList.contains('self-card') && !M.isRemoteHostReachableForControl(card)) {
        btn.disabled = true;
        btn.title = M.remoteHostUnreachableReason(card);
        toggleCardVariantSelector(card, false);
        continue;
      }
      var hostVersion = card.getAttribute('data-host-version') || '';
      var ip = card.getAttribute('data-host-ip') || '';
      var st = M.remoteUpdateStatusByIP[ip];

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

      if (M.hasUploadableSelection()) {
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
    M.updateReuseConfigVisibility();
    M.updateAllRemoteServiceControlButtons();
    M.refreshRemoteBulkButtonsState();
  }

  function toggleCardVariantSelector(card, show) {
    if (!card) return;
    var sel = card.querySelector('.card-variant-selector');
    if (!sel) return;
    sel.hidden = !show;
    if (show) {
      M.setVariantRadioSelection(sel, card.getAttribute('data-build-variant'));
    }
  }

  function doRemoveUpload() {
    var version = M.lastUpdateStatus.remove_version;
    if (!version) return;
    var status = M.el('upload-status');
    var removeBtn = M.el('remove-upload-btn');
    if (removeBtn) removeBtn.disabled = true;
    status.textContent = '스테이징에서 버전 삭제 중…';
    fetch(M.API_BASE + '/upload/remove', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ version: version })
    })
      .then(function (res) { return res.json(); })
      .then(function (body) {
        if (body.status === 'success') {
          status.textContent = body.data || '스테이징에서 삭제되었습니다.';
          M.lastUpdateStatus = {
            can_apply: false,
            apply_version: '',
            staging_versions: [],
            remove_version: '',
            staging_dual_agents: false
          };
          var variantFs = M.el('agent-variant-fieldset');
          if (variantFs) variantFs.hidden = true;
          var applyBtn = M.el('apply-update-btn');
          if (applyBtn) applyBtn.disabled = true;
          var stagingDisplay = M.el('staging-version-display');
          if (stagingDisplay) stagingDisplay.textContent = '';
          M.updateReuseConfigVisibility();
          updateAllHostApplyButtons();
          M.fetchUpdateStatus();
        } else {
          status.textContent = body.data || '삭제 실패.';
          M.fetchUpdateStatus();
        }
      })
      .catch(function () {
        status.textContent = '요청 실패.';
        M.fetchUpdateStatus();
      });
  }

  // exports
  M.canApplyToThisRemoteHostLegacy = canApplyToThisRemoteHostLegacy;
  M.doRemoveUpload = doRemoveUpload;
  M.getApplicableVersion = getApplicableVersion;
  M.getApplyButtonTitle = getApplyButtonTitle;
  M.pollUntilHostJsonOk = pollUntilHostJsonOk;
  M.refreshAllPanelsAfterUpdate = refreshAllPanelsAfterUpdate;
  M.scheduleRefreshAfterApply = scheduleRefreshAfterApply;
  M.scheduleRefreshAfterSwitchCurrent = scheduleRefreshAfterSwitchCurrent;
  M.toggleCardVariantSelector = toggleCardVariantSelector;
  M.updateAllHostApplyButtons = updateAllHostApplyButtons;
})(window.MolMaintenance = window.MolMaintenance || {});
