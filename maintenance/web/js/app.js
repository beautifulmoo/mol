/* eslint-disable */
(function (M) {
'use strict';
  function fetchUpdateStatusForRemote(ip) {
    if (!ip) return;
    M.remoteUpdateStatusByIP[ip] = { pending: true, ok: false };
    fetch(M.API_BASE + '/update-status?ip=' + encodeURIComponent(ip))
      .then(function (res) { return res.json(); })
      .then(function (body) {
        if (body.status !== 'success' || !body.data) {
          M.remoteUpdateStatusByIP[ip] = { pending: false, ok: false, err: true, can_apply: false, apply_version: '' };
          M.updateAllHostApplyButtons();
          return;
        }
        var d = body.data;
        M.remoteUpdateStatusByIP[ip] = {
          pending: false,
          ok: true,
          can_apply: !!d.can_apply,
          apply_version: d.apply_version || '',
          remote_version: d.remote_current_version || ''
        };
        M.updateAllHostApplyButtons();
      })
      .catch(function () {
        M.remoteUpdateStatusByIP[ip] = { pending: false, ok: false, err: true, can_apply: false, apply_version: '' };
        M.updateAllHostApplyButtons();
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

  function fetchRollbackStatusForRemote(ip) {
    if (!ip) return;
    M.remoteRollbackStatusByIP[ip] = { pending: true, ok: false };
    fetch(M.API_BASE + '/versions/list?ip=' + encodeURIComponent(ip))
      .then(function (res) { return res.json(); })
      .then(function (body) {
        if (body.status !== 'success' || !body.data || !body.data.versions) {
          M.remoteRollbackStatusByIP[ip] = { pending: false, ok: false, err: true, can_rollback: false };
          M.refreshRemoteBulkButtonsState();
          return;
        }
        M.remoteRollbackStatusByIP[ip] = {
          pending: false,
          ok: true,
          can_rollback: M.canRollbackFromVersionsList(body.data.versions)
        };
        M.refreshRemoteBulkButtonsState();
      })
      .catch(function () {
        M.remoteRollbackStatusByIP[ip] = { pending: false, ok: false, err: true, can_rollback: false };
        M.refreshRemoteBulkButtonsState();
      });
  }

  function fetchRollbackStatusForAllRemoteHosts() {
    var cards = document.querySelectorAll('#discovered-hosts .host-card:not(.self-card)');
    var seen = {};
    for (var i = 0; i < cards.length; i++) {
      var hip = cards[i].getAttribute('data-host-ip');
      if (hip && !seen[hip]) {
        seen[hip] = true;
        fetchRollbackStatusForRemote(hip);
      }
    }
  }

  function fetchUpdateStatus() {
    fetch(M.API_BASE + '/update-status')
      .then(function (res) { return res.json(); })
      .then(function (body) {
        if (body.status !== 'success' || !body.data) return;
        var d = body.data;
        M.lastUpdateStatus = {
          can_apply: !!d.can_apply,
          apply_version: d.apply_version || '',
          staging_versions: d.staging_versions || [],
          remove_version: d.remove_version || '',
          staging_dual_agents: !!d.staging_dual_agents
        };
        var variantFs = M.el('agent-variant-fieldset');
        if (variantFs) {
          variantFs.hidden = !M.lastUpdateStatus.staging_dual_agents;
          if (!variantFs.hidden) M.applyLocalVariantDefault();
        }
        var applyBtn = M.el('apply-update-btn');
        var removeBtn = M.el('remove-upload-btn');
        var stagingDisplay = M.el('staging-version-display');
        if (applyBtn) applyBtn.disabled = !M.lastUpdateStatus.can_apply;
        if (removeBtn) removeBtn.disabled = !(M.lastUpdateStatus.staging_versions && M.lastUpdateStatus.staging_versions.length > 0);
        if (stagingDisplay) {
          stagingDisplay.textContent = M.lastUpdateStatus.staging_versions && M.lastUpdateStatus.staging_versions.length > 0
            ? '스테이징: ' + M.lastUpdateStatus.staging_versions.join(', ')
            : '';
        }
        M.updateReuseConfigVisibility();
        M.updateAllHostApplyButtons();
        fetchUpdateStatusForAllRemoteHosts();
        fetchRollbackStatusForAllRemoteHosts();
        M.refreshRemoteBulkButtonsState();
      })
      .catch(function () {});
  }

  function hasUploadableSelection() {
    var bundle = M.el('upload-bundle');
    return !!(bundle && bundle.files && bundle.files[0]);
  }

  function updateUploadButtonState() {
    var uploadBtn = M.el('upload-btn');
    if (!uploadBtn) return;
    uploadBtn.disabled = !hasUploadableSelection();
    M.updateAllHostApplyButtons();
  }

  function resetUploadForm() {
    var bundleInput = M.el('upload-bundle');
    if (bundleInput) { bundleInput.value = ''; }
    var uploadBtn = M.el('upload-btn');
    if (uploadBtn) uploadBtn.disabled = true;
    M.updateAllHostApplyButtons();
  }

  function doUpload() {
    var bundleInput = M.el('upload-bundle');
    var status = M.el('upload-status');
    var applyBtn = M.el('apply-update-btn');
    if (!bundleInput || !bundleInput.files[0]) {
      status.textContent = 'tar.gz 번들 파일을 선택하세요.';
      return;
    }
    var formData = new FormData();
    formData.append('bundle', bundleInput.files[0]);
    status.textContent = '업로드 중(번들 검증)…';
    fetch(M.API_BASE + '/upload', {
      method: 'POST',
      body: formData
    })
      .then(function (res) { return res.json(); })
      .then(function (body) {
        if (body.status === 'success' && body.data && body.data.version) {
          M.lastUploadedVersion = body.data.version;
          status.textContent = '버전 ' + body.data.version + ' 스테이징에 업로드됨. 같은 버전으로 원격 적용 가능.';
          fetchUpdateStatus();
          M.updateAllHostApplyButtons();
        } else {
          status.textContent = body.data || '업로드 실패.';
        }
      })
      .catch(function () {
        status.textContent = '업로드 요청 실패.';
      });
  }

  function doApplyUpdate() {
    var version = M.lastUpdateStatus.apply_version;
    if (!version || !M.lastUpdateStatus.can_apply) return;
    var status = M.el('apply-update-status');
    var applyBtn = M.el('apply-update-btn');
    var selfCard = M.el('self-info') && M.el('self-info').querySelector('.host-card');
    var reusePreviousConfig = M.getReusePreviousConfig();
    var reuseCheckboxVisible = M.isLocalReuseConfigVisible();

    M.confirmApplyConfigChoice(reusePreviousConfig, reuseCheckboxVisible, function () {
      if (applyBtn) applyBtn.disabled = true;
      if (status) status.textContent = '업데이트 적용 요청 중…';
      M.showCardUpdating(selfCard, true);
      fetch(M.API_BASE + '/apply-update', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          version: version,
          agent_variant: M.getSelectedAgentVariant(),
          reuse_previous_config: reusePreviousConfig
        })
      })
        .then(function (res) { return res.json(); })
        .then(function (body) {
          if (body.status === 'success') {
            fetchUpdateStatus();
            if (status) status.textContent = '업데이트 적용 중… 재시작 후 정보를 자동으로 불러옵니다.';
            M.startUpdateLogPolling(version, selfCard, '');
            M.pollUntilHostJsonOk(M.API_BASE + '/self', 15, 4000, 2000, function (body2) {
              if (selfCard) M.updateHostCardDetails(selfCard, body2.data);
              M.refreshAllPanelsAfterUpdate(selfCard, '');
              M.updateAllHostApplyButtons();
              if (selfCard) M.showCardUpdating(selfCard, false);
              if (status) status.textContent = '적용 완료. 업데이트 기록·config·버전·상태를 반영했습니다.';
              if (applyBtn) fetchUpdateStatus();
            }, function (networkFailure) {
              if (selfCard) M.refreshAllPanelsAfterUpdate(selfCard, '');
              M.showCardUpdating(selfCard, false);
              if (status) {
                status.textContent = networkFailure
                  ? '연결 실패. 페이지를 새로고침해 보세요.'
                  : '서버 응답이 지연됩니다. 잠시 후 새로고침하세요.';
              }
              if (applyBtn) fetchUpdateStatus();
            });
          } else {
            if (status) status.textContent = body.data || '적용 실패.';
            M.showCardUpdating(selfCard, false);
            fetchUpdateStatus();
          }
        })
        .catch(function () {
          if (status) status.textContent = '요청 실패. 서버가 재시작 중일 수 있습니다. 잠시 후 페이지를 새로고침해 보세요.';
          M.showCardUpdating(selfCard, false);
          fetchUpdateStatus();
        });
    });
  }


  M.el('discovery-btn').addEventListener('click', M.runDiscovery);
  var pushConfigAllBtn = M.el('push-config-all-remotes-btn');
  if (pushConfigAllBtn) {
    pushConfigAllBtn.addEventListener('click', M.runPushConfigToAllRemotes);
  }
  var restartAllBtn = M.el('restart-all-remotes-btn');
  if (restartAllBtn) {
    restartAllBtn.addEventListener('click', M.runRestartAllRemotes);
  }
  var applyUpdateAllBtn = M.el('apply-update-all-remotes-btn');
  if (applyUpdateAllBtn) {
    applyUpdateAllBtn.addEventListener('click', M.runApplyUpdateAllRemotes);
  }
  var rollbackAllBtn = M.el('rollback-all-remotes-btn');
  if (rollbackAllBtn) {
    rollbackAllBtn.addEventListener('click', M.runRollbackAllRemotes);
  }
  M.el('upload-btn').addEventListener('click', doUpload);
  M.el('apply-update-btn').addEventListener('click', doApplyUpdate);
  M.el('reset-selection-btn').addEventListener('click', resetUploadForm);
  M.el('remove-upload-btn').addEventListener('click', M.doRemoveUpload);
  M.el('upload-bundle').addEventListener('change', updateUploadButtonState);

  resetUploadForm();
  fetchUpdateStatus();
  M.updateAllHostApplyButtons();
  M.initConfigEditorModal();
  M.initBulkResultsModal();
  M.initBulkApplyUpdateConfirmModal();
  M.loadSelf();

  document.addEventListener('visibilitychange', function () {
    if (document.hidden) {
      Object.keys(M.remoteHealthState).forEach(function (ip) {
        var st = M.remoteHealthState[ip];
        if (st && st.timerId != null) {
          clearTimeout(st.timerId);
          st.timerId = null;
        }
      });
    } else {
      Object.keys(M.remoteHealthState).forEach(function (ip) {
        M.scheduleRemoteHealthTick(ip);
      });
    }
  });
  setTimeout(function () { M.enumerateDiscoveredRemoteHealth(); }, 0);
  M.refreshRemoteBulkButtonsState();

  // exports
  M.doApplyUpdate = doApplyUpdate;
  M.doUpload = doUpload;
  M.fetchRollbackStatusForAllRemoteHosts = fetchRollbackStatusForAllRemoteHosts;
  M.fetchRollbackStatusForRemote = fetchRollbackStatusForRemote;
  M.fetchUpdateStatus = fetchUpdateStatus;
  M.fetchUpdateStatusForAllRemoteHosts = fetchUpdateStatusForAllRemoteHosts;
  M.fetchUpdateStatusForRemote = fetchUpdateStatusForRemote;
  M.hasUploadableSelection = hasUploadableSelection;
  M.resetUploadForm = resetUploadForm;
  M.updateUploadButtonState = updateUploadButtonState;

})(window.MolMaintenance = window.MolMaintenance || {});
