/* eslint-disable */
(function (M) {
'use strict';
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
      M.updateAllHostApplyButtons();
    }
  }

  function scheduleRemoteHealthTick(ip) {
    var st = M.remoteHealthState[ip];
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
    fetch(M.API_BASE + '/host-info?ip=' + encodeURIComponent(ip))
      .then(function (res) { return res.json(); })
      .then(function (body) {
        if (body.status === 'success' && body.data) {
          M.updateHostCardDetails(card, body.data);
          M.mergeHostIpsFromResponseIntoCard(card, body.data);
          if (body.data.responded_from_ip) M.mergeRespondedFromIntoCard(card, body.data.responded_from_ip);
          var row = card.closest && card.closest('.host-row');
          if (row) M.updateHostRowLabel(row, body.data, false);
          M.fetchUpdateLogForCard(card, ip);
          M.fetchCurrentConfigForCard(card, ip);
          M.fetchVersionsListForCard(card, ip);
          M.fetchServiceStatus(card, ip);
          M.fetchUpdateStatus();
          M.updateAllHostApplyButtons();
        }
      })
      .catch(function () {});
  }

  function onRemoteHealthTransportFail(ip, card, detail) {
    var st = M.remoteHealthState[ip];
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
    var list = M.el('discovered-hosts');
    var card = list ? M.findHostCardByIp(list, ip) : null;
    if (!card) {
      delete M.remoteHealthState[ip];
      return;
    }
    var btn = card.querySelector('.remote-health-recheck-btn');
    if (manual && btn) btn.disabled = true;
    fetch(M.API_BASE + '/remote-health-check?ip=' + encodeURIComponent(ip))
      .then(function (res) { return res.json(); })
      .then(function (body) {
        var st = M.remoteHealthState[ip];
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
    if (!M.remoteHealthState[ip]) {
      M.remoteHealthState[ip] = { failures: 0, timerId: null, dead: false };
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
    var list = M.el('discovered-hosts');
    if (!list) return;
    var cards = list.querySelectorAll('.host-card');
    for (var i = 0; i < cards.length; i++) {
      registerRemoteHealthMonitoring(cards[i]);
    }
  }

  // exports
  M.bindRemoteHealthForCard = bindRemoteHealthForCard;
  M.ensureRemoteHealthForIp = ensureRemoteHealthForIp;
  M.enumerateDiscoveredRemoteHealth = enumerateDiscoveredRemoteHealth;
  M.execRemoteHealthCheck = execRemoteHealthCheck;
  M.getRemoteHealthCfg = getRemoteHealthCfg;
  M.onRemoteHealthTransportFail = onRemoteHealthTransportFail;
  M.refreshRemoteHostAfterHealthOk = refreshRemoteHostAfterHealthOk;
  M.registerRemoteHealthMonitoring = registerRemoteHealthMonitoring;
  M.scheduleRemoteHealthTick = scheduleRemoteHealthTick;
  M.setRemoteHealthCardUI = setRemoteHealthCardUI;
})(window.MolMaintenance = window.MolMaintenance || {});
