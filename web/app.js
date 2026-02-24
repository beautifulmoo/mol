(function () {
  const API_BASE = '/api/v1';

  const serverIconSvg = '<svg class="host-icon" xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="currentColor" aria-hidden="true"><rect x="2" y="4" width="20" height="4" rx="1"/><rect x="2" y="10" width="20" height="4" rx="1"/><rect x="2" y="16" width="20" height="4" rx="1"/><circle cx="6" cy="6" r="0.8"/><circle cx="6" cy="12" r="0.8"/><circle cx="6" cy="18" r="0.8"/></svg>';

  function el(id) {
    return document.getElementById(id);
  }

  function renderHostCard(host, isSelf) {
    const div = document.createElement('div');
    div.className = 'host-card' + (isSelf ? ' self-card' : '');
    if (host.host_ip) {
      div.setAttribute('data-host-ip', host.host_ip);
    }
    div.setAttribute('data-host-version', host.version || '');
    var statusBlock =
      '<dt>systemctl status</dt><dd class="service-status-dd">' +
      '<div class="service-status-block">' +
      '<div class="service-status-header" role="button" tabindex="0" aria-expanded="false">' +
      '<span class="service-status-icon" aria-hidden="true">▶</span> ' +
      '<span class="service-status-summary">불러오는 중…</span>' +
      '</div>' +
      '<pre class="service-status-output"></pre>' +
      '</div></dd>';
    var controlBlock = isSelf
      ? ('<div class="service-control">' +
          '<button type="button" class="service-btn status-refresh-btn">상태 새로고침</button>' +
          '</div>')
      : ('<div class="service-control">' +
          '<button type="button" class="service-btn host-control-start">시작</button>' +
          '<button type="button" class="service-btn service-stop">중지</button>' +
          '<button type="button" class="service-btn service-start apply-update-host" disabled>업데이트 적용</button>' +
          '<button type="button" class="service-btn status-refresh-btn">상태 새로고침</button>' +
          '</div>');
    div.innerHTML =
      '<div class="updating-indicator" role="status" aria-label="업데이트 적용 중"></div>' +
      '<div class="host-icon">' + serverIconSvg + '</div>' +
      '<dl class="host-details">' +
      '<dt>버전</dt><dd>' + escapeHtml(host.version || '-') + '</dd>' +
      '<dt>IP</dt><dd>' + escapeHtml(host.host_ip || '-') + '</dd>' +
      '<dt>호스트명</dt><dd>' + escapeHtml(host.hostname || '-') + '</dd>' +
      '<dt>서비스 포트</dt><dd>' + (host.service_port != null ? host.service_port : '-') + '</dd>' +
      '<dt>CPU</dt><dd>' + escapeHtml(host.cpu_info || '-') + (host.cpu_usage_percent != null ? ' (' + host.cpu_usage_percent.toFixed(1) + '%)' : '') + '</dd>' +
      '<dt>메모리</dt><dd>' + formatMemory(host) + '</dd>' +
      statusBlock +
      '</dl>' +
      controlBlock;
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
        if (isSelf) {
          loadSelf();
          return;
        }
        if (summary) summary.textContent = '갱신 중…';
        fetch(API_BASE + '/host-info?ip=' + encodeURIComponent(ip))
          .then(function (res) { return res.json(); })
          .then(function (body) {
            if (body.status === 'success' && body.data) {
              updateHostCardDetails(cardEl, body.data);
              updateAllHostApplyButtons();
            } else {
              if (summary) summary.textContent = body.data || '호스트 정보를 불러올 수 없습니다.';
            }
            fetchServiceStatus(cardEl, ip);
          })
          .catch(function () {
            if (summary) summary.textContent = '호스트 정보 요청 실패.';
            fetchServiceStatus(cardEl, ip);
          });
      });
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
                if (summary) summary.textContent = body.data || '적용 완료. 잠시 후 상태를 다시 읽어옵니다.';
                fetchServiceStatus(cardEl, ip);
                setTimeout(function () {
                  fetch(API_BASE + '/host-info?ip=' + encodeURIComponent(ip))
                    .then(function (res) { return res.json(); })
                    .then(function (body) {
                      if (body.status === 'success' && body.data) {
                        updateHostCardDetails(cardEl, body.data);
                        updateAllHostApplyButtons();
                      }
                      fetchServiceStatus(cardEl, ip);
                      showCardUpdating(cardEl, false);
                    })
                    .catch(function () {
                      fetchServiceStatus(cardEl, ip);
                      showCardUpdating(cardEl, false);
                    });
                }, 5000);
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
        // mol+config 선택된 경우: 로컬에 저장하지 않고 원격으로만 전송 (multipart apply-update)
        var molInput = el('upload-mol');
        var configEditor = el('upload-config-editor');
        if (!molInput || !molInput.files[0] || !configEditor || !configEditor.value.trim()) {
          if (summary) summary.textContent = 'mol과 config.yaml을 선택하세요.';
          recheckApplyButton();
          return;
        }
        var formData = new FormData();
        formData.append('ip', ip);
        formData.append('mol', molInput.files[0]);
        formData.append('config', new Blob([configEditor.value], { type: 'text/yaml' }), 'config.yaml');
        showCardUpdating(cardEl, true);
        fetch(API_BASE + '/apply-update', {
          method: 'POST',
          body: formData
        })
          .then(function (res) { return res.json(); })
          .then(function (body) {
            if (body.status === 'success') {
              if (summary) summary.textContent = body.data || '적용 완료. 잠시 후 상태를 다시 읽어옵니다.';
              fetchServiceStatus(cardEl, ip);
              setTimeout(function () {
                fetch(API_BASE + '/host-info?ip=' + encodeURIComponent(ip))
                  .then(function (res) { return res.json(); })
                  .then(function (body) {
                    if (body.status === 'success' && body.data) {
                      updateHostCardDetails(cardEl, body.data);
                      updateAllHostApplyButtons();
                    }
                    fetchServiceStatus(cardEl, ip);
                    showCardUpdating(cardEl, false);
                  })
                  .catch(function () {
                    fetchServiceStatus(cardEl, ip);
                    showCardUpdating(cardEl, false);
                  });
              }, 5000);
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
    var dds = cardEl.querySelectorAll('.host-details > dd');
    if (dds.length >= 6) {
      dds[0].textContent = host.version || '-';
      dds[1].textContent = host.host_ip || '-';
      dds[2].textContent = host.hostname || '-';
      dds[3].textContent = host.service_port != null ? host.service_port : '-';
      dds[4].innerHTML = escapeHtml(host.cpu_info || '-') + (host.cpu_usage_percent != null ? ' (' + host.cpu_usage_percent.toFixed(1) + '%)' : '');
      dds[5].textContent = formatMemory(host);
    }
  }

  function loadSelf() {
    const container = el('self-info');
    container.innerHTML = '<div class="host-loading">불러오는 중…</div>';
    fetch(API_BASE + '/self')
      .then(function (res) { return res.json(); })
      .then(function (body) {
        if (body.status === 'success' && body.data) {
          container.innerHTML = '';
          var card = renderHostCard(body.data, true);
          container.appendChild(card);
          bindServiceControlButtons(card);
          fetchServiceStatus(card, '');
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
      if (cards[i].getAttribute('data-host-ip') === ip) return cards[i];
    }
    return null;
  }

  function runDiscovery() {
    const btn = el('discovery-btn');
    const status = el('discovery-status');
    const list = el('discovered-hosts');
    if (!list) return;
    btn.disabled = true;
    status.textContent = 'Discovery 진행 중… (기존 호스트는 그대로 제어 가능)';
    var count = list.querySelectorAll('.host-card:not(.self-card)').length;
    var evtSource = new EventSource(API_BASE + '/discovery/stream');
    evtSource.onmessage = function (e) {
      try {
        var host = JSON.parse(e.data);
        var ip = host.host_ip || '';
        var existing = findHostCardByIp(list, ip);
        if (existing) {
          updateHostCardDetails(existing, host);
          fetchServiceStatus(existing, ip);
          updateAllHostApplyButtons();
        } else {
          var card = renderHostCard(host, false);
          list.appendChild(card);
          bindServiceControlButtons(card);
          fetchServiceStatus(card, ip);
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
      if (count === 0) {
        status.textContent = 'Discovery 요청 실패.';
      } else {
        status.textContent = '호스트 ' + count + '개 발견.';
      }
      updateAllHostApplyButtons();
    };
  }

  var lastUploadedVersion = '';
  var lastUpdateStatus = { can_apply: false, apply_version: '', staging_versions: [], remove_version: '' };

  var FILE_LABEL_NONE = '선택된 파일 없음';

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

  function updateFileLabel(inputId, labelId) {
    var input = el(inputId);
    var labelEl = el(labelId);
    if (!input || !labelEl) return;
    var name = input.files && input.files[0] ? input.files[0].name : '';
    labelEl.textContent = name || FILE_LABEL_NONE;
    updateUploadButtonState();
  }

  function hasUploadableSelection() {
    var molHas = el('upload-mol') && el('upload-mol').files && el('upload-mol').files[0];
    var configEditor = el('upload-config-editor');
    var configHas = configEditor && configEditor.value.trim();
    return !!(molHas && configHas);
  }

  function canApplyToRemote() {
    return !!lastUploadedVersion || hasUploadableSelection();
  }

  function updateUploadButtonState() {
    var uploadBtn = el('upload-btn');
    if (!uploadBtn) return;
    uploadBtn.disabled = !hasUploadableSelection();
    updateAllHostApplyButtons();
  }

  function loadConfigIntoEditor(file, callback) {
    var editor = el('upload-config-editor');
    if (!editor) { if (callback) callback(); return; }
    if (!file) {
      editor.value = '';
      if (callback) callback();
      return;
    }
    var reader = new FileReader();
    reader.onload = function () {
      editor.value = typeof reader.result === 'string' ? reader.result : '';
      if (callback) callback();
    };
    reader.onerror = function () {
      editor.value = '';
      if (callback) callback();
    };
    reader.readAsText(file, 'UTF-8');
  }

  function resetUploadForm() {
    var molInput = el('upload-mol');
    var configInput = el('upload-config');
    if (molInput) { molInput.value = ''; }
    if (configInput) { configInput.value = ''; }
    var molLabel = el('upload-mol-label');
    var configLabel = el('upload-config-label');
    if (molLabel) molLabel.textContent = FILE_LABEL_NONE;
    if (configLabel) configLabel.textContent = FILE_LABEL_NONE;
    var editor = el('upload-config-editor');
    if (editor) editor.value = '';
    var uploadBtn = el('upload-btn');
    if (uploadBtn) uploadBtn.disabled = true;
    updateAllHostApplyButtons();
  }

  function doUpload() {
    var molInput = el('upload-mol');
    var configEditor = el('upload-config-editor');
    var status = el('upload-status');
    var applyBtn = el('apply-update-btn');
    if (!molInput || !molInput.files[0]) {
      status.textContent = 'mol 실행 파일을 선택하세요.';
      return;
    }
    if (!configEditor || !configEditor.value.trim()) {
      status.textContent = 'config.yaml을 선택하거나 내용을 입력하세요.';
      return;
    }
    var formData = new FormData();
    formData.append('mol', molInput.files[0]);
    formData.append('config', new Blob([configEditor.value], { type: 'text/yaml' }), 'config.yaml');
    status.textContent = '업로드 중…';
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
          fetchUpdateLog();
          fetchUpdateStatus();
          var sec = 10;
          status.textContent = '업데이트 적용이 요청되었습니다. 서버가 재시작됩니다. ' + sec + '초 후 자동 새로고침…';
          var t = setInterval(function () {
            sec -= 1;
            if (sec <= 0) {
              clearInterval(t);
              status.textContent = '새로고침 중…';
              location.reload();
              return;
            }
            status.textContent = '업데이트 적용이 요청되었습니다. 서버가 재시작됩니다. ' + sec + '초 후 자동 새로고침…';
          }, 1000);
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

  function fetchUpdateLog() {
    var pre = el('update-log-output');
    if (!pre) return;
    pre.textContent = '불러오는 중…';
    fetch(API_BASE + '/update-log')
      .then(function (res) { return res.json(); })
      .then(function (body) {
        if (body.status === 'success' && body.data && body.data.output !== undefined) {
          pre.textContent = body.data.output || '(비어 있음)';
        } else {
          pre.textContent = body.data || '로그를 불러올 수 없습니다.';
        }
      })
      .catch(function () {
        pre.textContent = '로그를 불러올 수 없습니다.';
      });
  }

  el('discovery-btn').addEventListener('click', runDiscovery);
  el('upload-btn').addEventListener('click', doUpload);
  el('apply-update-btn').addEventListener('click', doApplyUpdate);
  el('reset-selection-btn').addEventListener('click', resetUploadForm);
  el('remove-upload-btn').addEventListener('click', doRemoveUpload);
  el('update-log-refresh-btn').addEventListener('click', fetchUpdateLog);
  el('upload-mol').addEventListener('change', function () { updateFileLabel('upload-mol', 'upload-mol-label'); });
  el('upload-config').addEventListener('change', function () {
    var configInput = el('upload-config');
    var file = configInput && configInput.files && configInput.files[0];
    loadConfigIntoEditor(file, function () {
      updateFileLabel('upload-config', 'upload-config-label');
    });
  });
  el('upload-config-editor').addEventListener('input', updateUploadButtonState);

  resetUploadForm();
  fetchUpdateStatus();
  updateAllHostApplyButtons();
  loadSelf();
  fetchUpdateLog();
})();
