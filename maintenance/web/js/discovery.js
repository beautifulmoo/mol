/* eslint-disable */
(function (M) {
'use strict';
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
    var list = M.el('discovered-hosts');
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

  function fetchRemoteBulkStatusForCardsNotInDiscoveryRun(respondedKeys) {
    var cards = document.querySelectorAll('#discovered-hosts .host-card:not(.self-card)');
    var seen = {};
    for (var i = 0; i < cards.length; i++) {
      var card = cards[i];
      var hip = card.getAttribute('data-host-ip');
      if (!hip || seen[hip]) continue;
      seen[hip] = true;
      if (respondedKeys[discoveryTrackKeyFromCard(card)]) continue;
      M.fetchUpdateStatusForRemote(hip);
      M.fetchRollbackStatusForRemote(hip);
    }
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

  function runDiscovery() {
    const btn = M.el('discovery-btn');
    const status = M.el('discovery-status');
    const list = M.el('discovered-hosts');
    if (!list) return;
    stopDiscoveryCountdown();
    btn.disabled = true;
    var count = list.querySelectorAll('.host-card:not(.self-card)').length;
    var discoveryRespondedKeys = {};
    startDiscoveryCountdown(status, count);
    var discoveryFailHandled = false;
    var evtSource = new EventSource(M.API_BASE + '/discovery/stream');
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
      M.updateAllHostApplyButtons();
    });
    evtSource.onmessage = function (e) {
      try {
        var host = JSON.parse(e.data);
        if (host.self) {
          var selfCard = M.el('self-info') && M.el('self-info').querySelector('.host-card');
          if (selfCard && host.responded_from_ip) {
            M.mergeRespondedFromIntoCard(selfCard, host.responded_from_ip);
            var selfRow = M.el('self-info').querySelector('.host-row');
            if (selfRow) M.updateHostRowLabel(selfRow, { hostname: host.hostname || selfCard.getAttribute('data-hostname') || '', responded_from_ip: host.responded_from_ip }, true);
          }
          return;
        }
        var ip = host.host_ip || '';
        var cpuUuid = (host.cpu_uuid || '').trim();
        var existing = null;
        if (cpuUuid) existing = M.findHostCardByCpuUuid(list, cpuUuid);
        if (!existing && ip) existing = M.findHostCardByIp(list, ip);
        var trackKey = discoveryTrackKeyFromHost(host);
        if (trackKey) discoveryRespondedKeys[trackKey] = true;
        /* hostname으로는 기존 카드를 찾지 않음: 서로 다른 호스트가 같은 hostname(예: kt-vm)을 쓰면 한 카드로 잘못 병합됨 */
        if (existing) {
          if (cpuUuid) existing.setAttribute('data-cpu-uuid', cpuUuid);
          if (host.hostname) existing.setAttribute('data-hostname', host.hostname);
          M.mergeHostIpsFromResponseIntoCard(existing, host);
          if (host.responded_from_ip) M.mergeRespondedFromIntoCard(existing, host.responded_from_ip);
          M.updateHostCardDetails(existing, host);
          var row = existing.closest && existing.closest('.host-row');
          if (row) M.updateHostRowLabel(row, host, false);
          var primaryIp = existing.getAttribute('data-host-ip') || ip;
          M.fetchServiceStatus(existing, primaryIp);
          M.fetchUpdateStatusForRemote(primaryIp);
          M.fetchRollbackStatusForRemote(primaryIp);
          M.updateAllHostApplyButtons();
          M.registerRemoteHealthMonitoring(existing);
          setDiscoveryMissUI(row, false);
        } else {
          var row = M.renderHostRow(host, false);
          list.appendChild(row);
          refreshRemoteHostRowTints();
          var card = row.querySelector('.host-card');
          M.bindServiceControlButtons(card);
          M.fetchServiceStatus(card, ip);
          M.fetchUpdateStatusForRemote(ip);
          M.fetchRollbackStatusForRemote(ip);
          M.registerRemoteHealthMonitoring(card);
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
      fetchRemoteBulkStatusForCardsNotInDiscoveryRun(discoveryRespondedKeys);
      M.updateAllHostApplyButtons();
      M.refreshRemoteBulkButtonsState();
    });
    evtSource.onerror = function () {
      evtSource.close();
      stopDiscoveryCountdown();
      btn.disabled = false;
      if (discoveryFailHandled) {
        M.updateAllHostApplyButtons();
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
      M.updateAllHostApplyButtons();
      M.refreshRemoteBulkButtonsState();
    };
  }

  // exports
  M.clearAllDiscoveryMissMarks = clearAllDiscoveryMissMarks;
  M.discoveryTrackKeyFromCard = discoveryTrackKeyFromCard;
  M.discoveryTrackKeyFromHost = discoveryTrackKeyFromHost;
  M.fetchRemoteBulkStatusForCardsNotInDiscoveryRun = fetchRemoteBulkStatusForCardsNotInDiscoveryRun;
  M.finalizeDiscoveryMissMarks = finalizeDiscoveryMissMarks;
  M.formatDiscoveryDoneStatus = formatDiscoveryDoneStatus;
  M.formatDiscoveryProgressStatus = formatDiscoveryProgressStatus;
  M.getDiscoveryTimeoutSec = getDiscoveryTimeoutSec;
  M.isRemoteHostReachableForControl = isRemoteHostReachableForControl;
  M.refreshDiscoveryProgressStatus = refreshDiscoveryProgressStatus;
  M.refreshRemoteHostRowTints = refreshRemoteHostRowTints;
  M.remoteHostUnreachableReason = remoteHostUnreachableReason;
  M.runDiscovery = runDiscovery;
  M.setDiscoveryMissUI = setDiscoveryMissUI;
  M.startDiscoveryCountdown = startDiscoveryCountdown;
  M.stopDiscoveryCountdown = stopDiscoveryCountdown;
  M.updateAllRemoteServiceControlButtons = updateAllRemoteServiceControlButtons;
  M.updateRemoteServiceControlButtons = updateRemoteServiceControlButtons;

})(window.MolMaintenance = window.MolMaintenance || {});
