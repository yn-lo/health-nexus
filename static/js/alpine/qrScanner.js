var _scannerState = { stream: null, scanning: false };

function startScanner(component) {
    var video = document.getElementById('scanner-video');

    navigator.mediaDevices.getUserMedia({ video: { facingMode: 'environment' } })
        .then(function(stream) {
            _scannerState.stream = stream;
            video.srcObject = stream;
            _scannerState.scanning = true;
            component.cameraActive = true;
            component.scannerResult = '';
            _scanLoop(video, component);
        })
        .catch(function(err) {
            component.cameraActive = false;
            component.scannerResult = '无法访问摄像头：' + err.message;
        });
}

function _scanLoop(video, component) {
    if (!_scannerState.scanning) return;

    if ('BarcodeDetector' in window) {
        var detector = new BarcodeDetector({ formats: ['qr_code'] });
        detector.detect(video)
            .then(function(barcodes) {
                if (barcodes.length > 0) {
                    _handleScannedValue(barcodes[0].rawValue, component);
                } else {
                    requestAnimationFrame(function() { _scanLoop(video, component); });
                }
            })
            .catch(function() {
                requestAnimationFrame(function() { _scanLoop(video, component); });
            });
    } else {
        component.scannerResult = '当前浏览器不支持扫码，请使用 Chrome 或 Safari';
        _stopCameraInternal(component);
    }
}

function _handleScannedValue(value, component) {
    _stopCameraInternal(component);
    var match = value.match(/\/accounts\/bind\/([A-Za-z0-9]+)\//);
    if (match) {
        window.location.href = '/accounts/bind/' + match[1] + '/';
    } else if (/^[A-Za-z0-9]{4,8}$/.test(value)) {
        window.location.href = '/accounts/bind/' + value + '/';
    } else {
        component.scannerResult = '未识别到有效的绑定码';
    }
}

function _stopCameraInternal(component) {
    _scannerState.scanning = false;
    if (_scannerState.stream) {
        _scannerState.stream.getTracks().forEach(function(track) { track.stop(); });
        _scannerState.stream = null;
    }
    var video = document.getElementById('scanner-video');
    if (video) video.srcObject = null;
    if (component) component.cameraActive = false;
}

function stopScanner(component) {
    _stopCameraInternal(component || null);
}

function submitManualCode(code) {
    if (!code || !code.trim()) return;
    window.location.href = '/accounts/bind/' + code.trim() + '/';
}
