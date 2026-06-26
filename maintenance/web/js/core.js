/* eslint-disable */
(function (M) {
'use strict';

  var _api = (typeof window !== 'undefined' && window.__CONTRABASS_API_PREFIX__) || '/api/v1';
  if (typeof _api === 'string' && _api.length > 1 && _api.charAt(_api.length - 1) === '/') {
    _api = _api.slice(0, -1);
  }
  var API_BASE = _api;

  M.API_BASE = API_BASE;
  M.lastUploadedVersion = '';
  M.lastUpdateStatus = { can_apply: false, apply_version: '', staging_versions: [], remove_version: '' };
  M.remoteUpdateStatusByIP = {};
  M.remoteRollbackStatusByIP = {};
  M.remoteHealthState = {};

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
    return !!(M.lastUpdateStatus.staging_versions && M.lastUpdateStatus.staging_versions.length > 0);
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

  // exports
  M.applyLocalVariantDefault = applyLocalVariantDefault;
  M.cardReuseConfigHtml = cardReuseConfigHtml;
  M.cardVariantRadiosHtml = cardVariantRadiosHtml;
  M.confirmApplyConfigChoice = confirmApplyConfigChoice;
  M.defaultAgentVariantFromBuild = defaultAgentVariantFromBuild;
  M.el = el;
  M.getCardAgentVariant = getCardAgentVariant;
  M.getCardReusePreviousConfig = getCardReusePreviousConfig;
  M.getHostRowLabel = getHostRowLabel;
  M.getReusePreviousConfig = getReusePreviousConfig;
  M.getSelectedAgentVariant = getSelectedAgentVariant;
  M.hasStagingOnServer = hasStagingOnServer;
  M.isCardReuseConfigVisible = isCardReuseConfigVisible;
  M.isLocalReuseConfigVisible = isLocalReuseConfigVisible;
  M.setVariantRadioSelection = setVariantRadioSelection;
  M.updateReuseConfigVisibility = updateReuseConfigVisibility;

})(window.MolMaintenance = window.MolMaintenance || {});
