/* eslint-disable */
(function (M) {
'use strict';

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

  function hostEntryFromCard(card) {
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
    if (ips.length === 0) return null;
    if (!primary) primary = ips[0];
    return {
      primary_ip: primary,
      hostname: (card.getAttribute('data-hostname') || '').trim(),
      cpu_uuid: (card.getAttribute('data-cpu-uuid') || '').trim(),
      ips: ips
    };
  }

  /** Collect bulk API hosts[] from remote cards. opts.reachableOnly skips dead / discovery-missed rows. */
  function collectRemoteHostsFromDOM(opts) {
    opts = opts || {};
    var reachableOnly = !!opts.reachableOnly;
    var list = M.el('discovered-hosts');
    if (!list) return [];
    var cards = list.querySelectorAll('.host-card:not(.self-card)');
    var hosts = [];
    for (var i = 0; i < cards.length; i++) {
      var card = cards[i];
      if (reachableOnly && M.isRemoteHostReachableForControl && !M.isRemoteHostReachableForControl(card)) {
        continue;
      }
      var entry = hostEntryFromCard(card);
      if (entry) hosts.push(entry);
    }
    return hosts;
  }

  M.collectRemoteHostsFromDOM = collectRemoteHostsFromDOM;
  M.mergeHostIpsFromResponseIntoCard = mergeHostIpsFromResponseIntoCard;
  M.mergeHostIpsIntoCard = mergeHostIpsIntoCard;
  M.mergeRespondedFromIntoCard = mergeRespondedFromIntoCard;

})(window.MolMaintenance = window.MolMaintenance || {});
