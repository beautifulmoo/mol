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
    var statusBlock =
      '<dt>systemctl status</dt><dd class="service-status-dd">' +
      '<div class="service-status-block">' +
      '<div class="service-status-header" role="button" tabindex="0" aria-expanded="false">' +
      '<span class="service-status-icon" aria-hidden="true">▶</span> ' +
      '<span class="service-status-summary">불러오는 중…</span>' +
      '</div>' +
      '<pre class="service-status-output"></pre>' +
      '</div></dd>';
    var controlBlock = isSelf ? '' : (
      '<div class="service-control">' +
      '<button type="button" class="service-btn service-start">시작</button>' +
      '<button type="button" class="service-btn service-stop">중지</button>' +
      '<button type="button" class="service-btn apply-update-host" disabled title="먼저 업데이트 영역에서 버전을 업로드하세요">업데이트 적용</button>' +
      '</div>'
    );
    div.innerHTML =
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
      var startBtn = cardEl.querySelector('.service-start');
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
    if (!cardEl || cardEl.classList.contains('self-card')) return;
    var ip = cardEl.getAttribute('data-host-ip') || '';
    var summary = cardEl.querySelector('.service-status-summary');
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
    var startBtn = cardEl.querySelector('.service-start');
    var stopBtn = cardEl.querySelector('.service-stop');
    if (startBtn) startBtn.addEventListener('click', function () { doControl('start'); });
    if (stopBtn) stopBtn.addEventListener('click', function () { doControl('stop'); });

    var applyHostBtn = cardEl.querySelector('.apply-update-host');
    if (applyHostBtn) {
      applyHostBtn.addEventListener('click', function () {
        if (!canApplyToRemote()) {
          if (summary) summary.textContent = 'mol과 config.yaml을 선택하세요.';
          return;
        }
        applyHostBtn.disabled = true;
        if (summary) summary.textContent = '업데이트 적용 중…';

        function doApplyToHost(version) {
          fetch(API_BASE + '/apply-update', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ version: version, ip: ip })
          })
            .then(function (res) { return res.json(); })
            .then(function (body) {
              if (body.status === 'success') {
                if (summary) summary.textContent = body.data || '적용 완료';
                fetchServiceStatus(cardEl, ip);
              } else {
                updateStatusUI(cardEl, null, body.data || '적용 실패');
              }
            })
            .catch(function () {
              updateStatusUI(cardEl, null, '요청 실패');
            })
            .finally(function () {
              applyHostBtn.disabled = !canApplyToRemote();
            });
        }

        if (lastUploadedVersion) {
          doApplyToHost(lastUploadedVersion);
          return;
        }
        // mol+config만 선택된 경우: 로컬에 저장하지 않고 원격으로만 전송 (multipart apply-update)
        var molInput = el('upload-mol');
        var configEditor = el('upload-config-editor');
        if (!molInput || !molInput.files[0] || !configEditor || !configEditor.value.trim()) {
          if (summary) summary.textContent = 'mol과 config.yaml을 선택하세요.';
          applyHostBtn.disabled = false;
          return;
        }
        var formData = new FormData();
        formData.append('ip', ip);
        formData.append('mol', molInput.files[0]);
        formData.append('config', new Blob([configEditor.value], { type: 'text/yaml' }), 'config.yaml');
        fetch(API_BASE + '/apply-update', {
          method: 'POST',
          body: formData
        })
          .then(function (res) { return res.json(); })
          .then(function (body) {
            if (body.status === 'success') {
              if (summary) summary.textContent = body.data || '적용 완료';
              fetchServiceStatus(cardEl, ip);
            } else {
              updateStatusUI(cardEl, null, body.data || '적용 실패');
            }
          })
          .catch(function () {
            updateStatusUI(cardEl, null, '요청 실패');
          })
          .finally(function () {
            applyHostBtn.disabled = !canApplyToRemote();
          });
      });
    }
  }

  function updateAllHostApplyButtons() {
    var enabled = canApplyToRemote();
    var btns = document.querySelectorAll('.apply-update-host');
    for (var i = 0; i < btns.length; i++) {
      btns[i].disabled = !enabled;
    }
    var removeBtn = el('remove-upload-btn');
    if (removeBtn) removeBtn.disabled = !lastUploadedVersion;
  }

  function doRemoveUpload() {
    if (!lastUploadedVersion) return;
    var status = el('upload-status');
    var removeBtn = el('remove-upload-btn');
    if (removeBtn) removeBtn.disabled = true;
    status.textContent = '서버에서 버전 삭제 중…';
    fetch(API_BASE + '/upload/remove', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ version: lastUploadedVersion })
    })
      .then(function (res) { return res.json(); })
      .then(function (body) {
        if (body.status === 'success') {
          lastUploadedVersion = '';
          status.textContent = body.data || '업로드된 버전이 서버에서 삭제되었습니다.';
          updateAllHostApplyButtons();
          var applyBtn = el('apply-update-btn');
          if (applyBtn) applyBtn.disabled = true;
        } else {
          status.textContent = body.data || '삭제 실패.';
          if (removeBtn) removeBtn.disabled = false;
        }
      })
      .catch(function () {
        status.textContent = '요청 실패.';
        if (removeBtn) removeBtn.disabled = false;
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

  function runDiscovery() {
    const btn = el('discovery-btn');
    const status = el('discovery-status');
    const list = el('discovered-hosts');
    btn.disabled = true;
    status.textContent = 'Discovery 진행 중… (응답 오는 대로 표시)';
    list.innerHTML = '';
    var count = 0;
    var evtSource = new EventSource(API_BASE + '/discovery/stream');
    evtSource.onmessage = function (e) {
      try {
        var host = JSON.parse(e.data);
        var card = renderHostCard(host, false);
        list.appendChild(card);
        bindServiceControlButtons(card);
        fetchServiceStatus(card, host.host_ip || '');
        count += 1;
        status.textContent = '호스트 ' + count + '개 발견 (계속 수신 중…)';
      } catch (err) {}
    };
    evtSource.addEventListener('done', function () {
      evtSource.close();
      btn.disabled = false;
      status.textContent = count ? '호스트 ' + count + '개 발견.' : 'Discovery 완료 (결과 없음).';
    });
    evtSource.onerror = function () {
      evtSource.close();
      btn.disabled = false;
      if (count === 0) {
        status.textContent = 'Discovery 요청 실패.';
      } else {
        status.textContent = '호스트 ' + count + '개 발견.';
      }
    };
  }

  var lastUploadedVersion = '';

  var FILE_LABEL_NONE = '선택된 파일 없음';

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
    applyBtn.disabled = true;
    fetch(API_BASE + '/upload', {
      method: 'POST',
      body: formData
    })
      .then(function (res) { return res.json(); })
      .then(function (body) {
        if (body.status === 'success' && body.data && body.data.version) {
          lastUploadedVersion = body.data.version;
          status.textContent = '버전 ' + body.data.version + ' 업로드됨. 같은 버전으로 로컬/원격 추가 적용 가능.';
          applyBtn.disabled = false;
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
    if (!lastUploadedVersion) return;
    var status = el('apply-update-status');
    status.textContent = '업데이트 적용 요청 중…';
    fetch(API_BASE + '/apply-update', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ version: lastUploadedVersion })
    })
      .then(function (res) { return res.json(); })
      .then(function (body) {
        if (body.status === 'success') {
          fetchUpdateLog();
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
        }
      })
      .catch(function () {
        status.textContent = '요청 실패. 서버가 재시작 중일 수 있습니다. 잠시 후 페이지를 새로고침해 보세요.';
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

  updateFileLabel('upload-mol', 'upload-mol-label');
  updateFileLabel('upload-config', 'upload-config-label');
  updateUploadButtonState();
  updateAllHostApplyButtons();
  loadSelf();
  fetchUpdateLog();
})();
