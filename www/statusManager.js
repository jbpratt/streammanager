export default class StatusManager {
    constructor() {
        this.autoHideTimeout = null;
    }

    showStatus(message, isSuccess, autoHide = true) {
        const statusDiv = document.getElementById('status');
        statusDiv.textContent = message;
        statusDiv.className = `mb-6 p-4 rounded-lg ${isSuccess ? 'bg-green-50 dark:bg-green-900/20 text-green-800 dark:text-green-200 border border-green-200 dark:border-green-800' : 'bg-red-50 dark:bg-red-900/20 text-red-800 dark:text-red-200 border border-red-200 dark:border-red-800'}`;
        statusDiv.classList.remove('hidden');

        if (autoHide) {
            this.clearAutoHideTimeout();
            this.autoHideTimeout = setTimeout(() => {
                statusDiv.classList.add('hidden');
            }, 5000);
        }
    }

    hideStatus() {
        const statusDiv = document.getElementById('status');
        statusDiv.classList.add('hidden');
        this.clearAutoHideTimeout();
    }

    clearAutoHideTimeout() {
        if (this.autoHideTimeout) {
            clearTimeout(this.autoHideTimeout);
            this.autoHideTimeout = null;
        }
    }

    showError(error) {
        const errorDiv = document.getElementById('errorNotification');

        if (error) {
            const errorTime = new Date(error.time * 1000).toLocaleTimeString();
            errorDiv.innerHTML = `
                <div class="bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 text-red-800 dark:text-red-200 px-4 py-3 rounded-lg">
                    <div class="font-semibold">⚠️ FFmpeg Error (${errorTime})</div>
                    <div class="mt-1 text-sm whitespace-pre-wrap">${error.message}</div>
                </div>
            `;
            errorDiv.classList.remove('hidden');
        } else {
            errorDiv.classList.add('hidden');
        }
    }

    showProgress(progress) {
        const progressDiv = document.getElementById('progress');
        progressDiv.classList.remove('hidden');

        // Update progress data
        document.getElementById('progressFrame').textContent = progress.frame || '-';
        document.getElementById('progressFps').textContent = progress.fps || '-';
        document.getElementById('progressBitrate').textContent = progress.bitrate || '-';
        document.getElementById('progressTime').textContent = progress.time || '-';
        document.getElementById('progressSpeed').textContent = progress.speed || '-';

        // Update timestamp
        if (progress.timestamp) {
            document.getElementById('progressTimestamp').textContent = new Date(progress.timestamp).toLocaleTimeString();
        }

        // Update progress bar
        if (progress.percentage !== undefined) {
            const progressBar = document.getElementById('progressBar');
            const progressPercentage = document.getElementById('progressPercentage');

            progressBar.style.width = progress.percentage.toFixed(1) + '%';
            progressPercentage.textContent = progress.percentage.toFixed(1) + '%';
        }
    }

    hideProgress() {
        const progressDiv = document.getElementById('progress');
        progressDiv.classList.add('hidden');

        // Reset progress values
        document.getElementById('progressFrame').textContent = '-';
        document.getElementById('progressFps').textContent = '-';
        document.getElementById('progressBitrate').textContent = '-';
        document.getElementById('progressTime').textContent = '-';
        document.getElementById('progressSpeed').textContent = '-';
        document.getElementById('progressTimestamp').textContent = '-';
        document.getElementById('progressBar').style.width = '0%';
        document.getElementById('progressPercentage').textContent = '0%';
    }

    showQueue(data) {
        const queueDiv = document.getElementById('queue');
        if (!queueDiv) {
            console.error('Queue element not found');
            return [];
        }
        const queueItems = data.queue || [];
        const status = data.status || {};

        let statusText = 'Stopped';
        let statusColor = 'text-gray-600 dark:text-gray-400';
        if (status.running) {
            statusText = status.activelyStreaming ? 'Streaming' : 'Ready';
            statusColor = status.activelyStreaming ? 'text-green-600 dark:text-green-400' : 'text-yellow-600 dark:text-yellow-400';
        }

        let html = `
            <div class="flex items-center justify-between mb-4 p-3 bg-gray-50 dark:bg-gray-700 rounded-lg">
                <div>
                    <span class="text-sm font-medium text-gray-700 dark:text-gray-300">Status:</span>
                    <span class="ml-2 font-semibold ${statusColor}">${statusText}</span>
                </div>
                <span class="text-sm text-gray-500 dark:text-gray-400">${queueItems.length} items</span>
            </div>
        `;

        if (status.playing) {
            html += `
                <div class="mb-4 p-3 bg-blue-50 dark:bg-blue-900/20 border border-blue-200 dark:border-blue-800 rounded-lg">
                    <div class="text-sm font-medium text-blue-800 dark:text-blue-200">Currently Playing:</div>
                    <div class="text-sm text-blue-600 dark:text-blue-300 mt-1">${status.playing.file}</div>
                </div>
            `;
        }

        if (queueItems.length === 0) {
            html += `
                <div class="text-center py-8 text-gray-500 dark:text-gray-400">
                    <div class="text-2xl mb-2">📂</div>
                    <div>No items in queue</div>
                    <div class="text-sm mt-1">Add files to start streaming</div>
                </div>
            `;
        } else {
            queueItems.forEach((item, index) => {
                html += `
                    <div class="flex justify-between items-center p-3 bg-gray-50 dark:bg-gray-700 border border-gray-200 dark:border-gray-600 rounded-lg mb-2">
                        <div class="flex-1">
                            <div class="text-sm font-medium text-gray-900 dark:text-white">${index + 1}. ${item.file}</div>
                        </div>
                        <button class="remove-btn bg-red-500 hover:bg-red-600 text-white px-3 py-1 rounded-lg text-sm font-medium transition-colors" data-id="${item.id}">
                            Remove
                        </button>
                    </div>
                `;
            });
        }

        queueDiv.innerHTML = html;
        return queueDiv.querySelectorAll('.remove-btn');
    }

    updateToggleButton(isRunning) {
        const toggleBtn = document.getElementById('toggleBtn');
        if (isRunning) {
            toggleBtn.textContent = 'Stop Stream';
            toggleBtn.className = 'w-full bg-red-500 text-white px-4 py-2 rounded hover:bg-red-600';
            toggleBtn.setAttribute('data-state', 'running');
        } else {
            toggleBtn.textContent = 'Start Stream';
            toggleBtn.className = 'w-full bg-green-500 text-white px-4 py-2 rounded hover:bg-green-600';
            toggleBtn.setAttribute('data-state', 'stopped');
        }
    }

    updateWebRTCStatus(message, className = 'text-gray-600') {
        const statusElement = document.getElementById('webrtcStatus');
        statusElement.textContent = message;
        statusElement.className = `text-sm ${className}`;
    }

    showWebRTCButtons(showConnect, showDisconnect) {
        const connectBtn = document.getElementById('whepConnect');
        const disconnectBtn = document.getElementById('whepDisconnect');

        connectBtn.classList.toggle('hidden', !showConnect);
        disconnectBtn.classList.toggle('hidden', !showDisconnect);
    }

    updateConnectionStats(connectionState, iceState) {
        if (connectionState) {
            document.getElementById('connectionState').textContent = connectionState;
        }
        if (iceState) {
            document.getElementById('iceState').textContent = iceState;
        }
    }

    showWebRTCStats(show) {
        const statsDiv = document.getElementById('webrtcStats');
        statsDiv.classList.toggle('hidden', !show);
    }

    updateServerFileSelection(selectedFile) {
        const enqueueBtn = document.getElementById('enqueueServerBtn');
        if (selectedFile) {
            enqueueBtn.disabled = false;
            enqueueBtn.textContent = `Add: ${selectedFile.split('/').pop()}`;
        } else {
            enqueueBtn.disabled = true;
            enqueueBtn.textContent = 'Add Selected File to Queue';
        }
    }
}
