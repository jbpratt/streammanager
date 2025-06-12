class StreamManager {
    constructor() {
        this.isRunning = false;
        this.player = null;
        this.progressInterval = null;
        this.lastProgressTime = null;
        this.currentPath = '.';
        this.selectedFile = null;
        this.fileMode = 'local'; // 'local' or 'server'

        // Subtitle browser properties
        this.subtitleCurrentPath = '.';
        this.selectedSubtitleFile = null;
        this.subtitleTargetInput = null;

        // WebRTC properties
        this.whepConnection = null;
        this.isWhepConnected = false;
        this.webrtcStatusInterval = null;

        // Theme management
        this.initTheme();
        
        this.initEventListeners();
    }

    initTheme() {
        // Check for saved theme or default to light
        const savedTheme = localStorage.getItem('theme');
        const prefersDark = window.matchMedia('(prefers-color-scheme: dark)').matches;
        const theme = savedTheme || (prefersDark ? 'dark' : 'light');
        
        this.setTheme(theme);
    }

    setTheme(theme) {
        const html = document.documentElement;
        const themeIcon = document.getElementById('themeIcon');
        
        if (theme === 'dark') {
            html.classList.add('dark');
            themeIcon.textContent = '☀️';
        } else {
            html.classList.remove('dark');
            themeIcon.textContent = '🌙';
        }
        
        localStorage.setItem('theme', theme);
    }

    toggleTheme() {
        const html = document.documentElement;
        const isDark = html.classList.contains('dark');
        this.setTheme(isDark ? 'light' : 'dark');
    }

    validateTimestamp(timestamp) {
        if (!timestamp) return true; // Empty is valid

        const regex = /^([0-1]?[0-9]|2[0-3]):[0-5][0-9]:[0-5][0-9]$/;
        return regex.test(timestamp);
    }

    initEventListeners() {
        // Theme toggle
        document.getElementById('themeToggle').addEventListener('click', () => this.toggleTheme());
        
        // Stream controls
        document.getElementById('toggleBtn').addEventListener('click', () => this.toggle());
        document.getElementById('enqueueLocalBtn').addEventListener('click', () => this.enqueueLocal());
        document.getElementById('enqueueServerBtn').addEventListener('click', () => this.enqueueServer());
        document.getElementById('skipBtn').addEventListener('click', () => this.skip());
        
        // Collapsible sections
        document.getElementById('configToggle').addEventListener('click', () => this.toggleConfigSettings());
        document.getElementById('overlayToggle').addEventListener('click', () => this.toggleOverlaySettings());
        document.getElementById('fileModeToggle').addEventListener('click', () => this.toggleFileModeSettings());
        
        // File management
        document.getElementById('showFilename').addEventListener('change', () => this.toggleOverlayOptions());
        document.getElementById('localFilesBtn').addEventListener('click', () => this.switchToLocalFiles());
        document.getElementById('serverFilesBtn').addEventListener('click', () => this.switchToServerFiles());
        document.getElementById('refreshBtn').addEventListener('click', () => this.refreshServerFiles());

        // Subtitle browser event listeners
        document.getElementById('browseSubtitleBtn').addEventListener('click', () => this.handleSubtitleBrowse('local'));
        document.getElementById('browseSubtitleServerBtn').addEventListener('click', () => this.handleSubtitleBrowse('server'));
        document.getElementById('closeSubtitleBrowser').addEventListener('click', () => this.closeSubtitleBrowser());
        document.getElementById('cancelSubtitleBtn').addEventListener('click', () => this.closeSubtitleBrowser());
        document.getElementById('selectSubtitleBtn').addEventListener('click', () => this.selectSubtitleFile());
        document.getElementById('refreshSubtitleBtn').addEventListener('click', () => this.refreshSubtitleFiles());

        // WebRTC event listeners
        document.getElementById('whepConnect').addEventListener('click', () => this.connectWHEP());
        document.getElementById('whepDisconnect').addEventListener('click', () => this.disconnectWHEP());

        // Auto-refresh queue on page load
        this.getQueue();

        // Set initial overlay options visibility
        this.toggleOverlayOptions();

        // Auto-refresh queue every 5 seconds
        setInterval(() => this.getQueue(), 5000);

        // Start progress polling
        this.startProgressPolling();

        // Start WebRTC status polling
        this.startWebRTCStatusPolling();
    }

    toggle() {
        if (this.isRunning) {
            this.stop();
        } else {
            this.start();
        }
    }

    toggleConfigSettings() {
        const content = document.getElementById('configContent');
        const arrow = document.getElementById('configArrow');

        if (content.style.display === 'none') {
            content.style.display = 'block';
            arrow.textContent = '▼';
        } else {
            content.style.display = 'none';
            arrow.textContent = '▶';
        }
    }

    toggleOverlaySettings() {
        const content = document.getElementById('overlayContent');
        const arrow = document.getElementById('overlayArrow');

        if (content.style.display === 'none') {
            content.style.display = 'block';
            arrow.textContent = '▼';
        } else {
            content.style.display = 'none';
            arrow.textContent = '▶';
        }
    }



    toggleOverlayOptions() {
        const checkbox = document.getElementById('showFilename');
        const options = document.getElementById('overlayOptions');

        if (checkbox.checked) {
            options.classList.remove('hidden');
        } else {
            options.classList.add('hidden');
        }
    }

    toggleFileModeSettings() {
        const content = document.getElementById('fileModeContent');
        const arrow = document.getElementById('fileModeArrow');

        if (content.style.display === 'none') {
            content.style.display = 'block';
            arrow.textContent = '▼';
        } else {
            content.style.display = 'none';
            arrow.textContent = '▶';
        }
    }

    switchToLocalFiles() {
        this.fileMode = 'local';
        document.getElementById('localFilesBtn').className = 'flex-1 px-4 py-2 text-sm font-medium text-primary-700 bg-white dark:bg-gray-600 dark:text-primary-300 rounded-md shadow-sm';
        document.getElementById('serverFilesBtn').className = 'flex-1 px-4 py-2 text-sm font-medium text-gray-700 dark:text-gray-300 hover:text-gray-900 dark:hover:text-white rounded-md';
        document.getElementById('localFileSection').classList.remove('hidden');
        document.getElementById('serverFileSection').classList.add('hidden');
    }

    switchToServerFiles() {
        this.fileMode = 'server';
        document.getElementById('localFilesBtn').className = 'flex-1 px-4 py-2 text-sm font-medium text-gray-700 dark:text-gray-300 hover:text-gray-900 dark:hover:text-white rounded-md';
        document.getElementById('serverFilesBtn').className = 'flex-1 px-4 py-2 text-sm font-medium text-primary-700 bg-white dark:bg-gray-600 dark:text-primary-300 rounded-md shadow-sm';
        document.getElementById('localFileSection').classList.add('hidden');
        document.getElementById('serverFileSection').classList.remove('hidden');
        this.loadServerFiles();
    }

    refreshServerFiles() {
        this.loadServerFiles();
    }

    updateToggleButton() {
        const toggleBtn = document.getElementById('toggleBtn');
        if (this.isRunning) {
            toggleBtn.textContent = 'Stop Stream';
            toggleBtn.className = 'w-full bg-red-500 text-white px-4 py-2 rounded hover:bg-red-600';
            toggleBtn.setAttribute('data-state', 'running');
        } else {
            toggleBtn.textContent = 'Start Stream';
            toggleBtn.className = 'w-full bg-green-500 text-white px-4 py-2 rounded hover:bg-green-600';
            toggleBtn.setAttribute('data-state', 'stopped');
        }
    }

    async start() {
        const destInput = document.getElementById('destInput');
        const dest = destInput.value.trim();
        const maxBitrate = document.getElementById('maxBitrate').value;
        const username = document.getElementById('username').value.trim();
        const password = document.getElementById('password').value.trim();
        const encoder = document.getElementById('encoder').value;
        const preset = document.getElementById('preset').value;
        const keyframeInterval = document.getElementById('keyframeInterval').value;

        if (!dest) {
            this.showStatus('Please enter a destination URL', false);
            return;
        }

        try {
            const response = await fetch('/start', {
                method: 'POST',
                headers: {
                    'Content-Type': 'application/json'
                },
                body: JSON.stringify({
                    destination: dest,
                    maxBitrate: maxBitrate,
                    username: username,
                    password: password,
                    encoder: encoder,
                    preset: preset,
                    keyframeInterval: keyframeInterval
                })
            });
            const text = await response.text();
            if (response.ok) {
                this.isRunning = true;
                this.updateToggleButton();
                this.getQueue(); // Refresh queue immediately
                this.startProgressPolling(); // Start progress updates
            }
            this.showStatus(text, response.ok);
        } catch (error) {
            this.showStatus('Error: ' + error.message, false);
        }
    }

    async enqueueLocal() {
        const fileInput = document.getElementById('fileInput');
        if (!fileInput.files.length) {
            this.showStatus('Please select a file first', false);
            return;
        }

        const fileName = fileInput.files[0].name;

        // Collect overlay settings
        const overlaySettings = {
            showFilename: document.getElementById('showFilename').checked,
            position: document.getElementById('overlayPosition').value,
            fontSize: parseInt(document.getElementById('fontSize').value)
        };

        // Get start timestamp from local file input
        const startTimestamp = document.getElementById('startTimestamp').value.trim();

        // Get subtitle file from local file input
        const subtitleFile = document.getElementById('subtitleFile').value.trim();

        // Validate timestamp format
        if (startTimestamp && !this.validateTimestamp(startTimestamp)) {
            this.showStatus('Start timestamp must be in HH:MM:SS format (e.g., 01:30:45)', false);
            return;
        }

        try {
            const response = await fetch('/enqueue', {
                method: 'POST',
                headers: {
                    'Content-Type': 'application/json'
                },
                body: JSON.stringify({
                    file: fileName,
                    overlay: overlaySettings,
                    startTimestamp: startTimestamp,
                    subtitleFile: subtitleFile
                })
            });

            if (response.ok) {
                const data = await response.json();
                this.showStatus(`File "${data.file}" enqueued with ID: ${data.id}`, true);
                fileInput.value = ''; // Clear the file input
                this.getQueue(); // Refresh queue immediately
            } else {
                const text = await response.text();
                this.showStatus(text, false);
            }
        } catch (error) {
            this.showStatus('Error: ' + error.message, false);
        }
    }

    async enqueueServer() {
        if (!this.selectedFile) {
            this.showStatus('Please select a server file first', false);
            return;
        }

        // Collect overlay settings
        const overlaySettings = {
            showFilename: document.getElementById('showFilename').checked,
            position: document.getElementById('overlayPosition').value,
            fontSize: parseInt(document.getElementById('fontSize').value)
        };

        // Get start timestamp from server file input
        const startTimestamp = document.getElementById('startTimestampServer').value.trim();

        // Get subtitle file from server file input
        const subtitleFile = document.getElementById('subtitleFileServer').value.trim();

        // Validate timestamp format
        if (startTimestamp && !this.validateTimestamp(startTimestamp)) {
            this.showStatus('Start timestamp must be in HH:MM:SS format (e.g., 01:30:45)', false);
            return;
        }

        try {
            const response = await fetch('/enqueue', {
                method: 'POST',
                headers: {
                    'Content-Type': 'application/json'
                },
                body: JSON.stringify({
                    file: this.selectedFile,
                    overlay: overlaySettings,
                    startTimestamp: startTimestamp,
                    subtitleFile: subtitleFile
                })
            });

            if (response.ok) {
                const data = await response.json();
                this.showStatus(`Server file "${data.file}" enqueued with ID: ${data.id}`, true);
                this.selectedFile = null;
                this.updateServerFileSelection();
                this.getQueue(); // Refresh queue immediately
            } else {
                const text = await response.text();
                this.showStatus(text, false);
            }
        } catch (error) {
            this.showStatus('Error: ' + error.message, false);
        }
    }

    async skip() {
        try {
            const response = await fetch('/skip', { method: 'POST' });
            const text = await response.text();
            this.showStatus(text, response.ok);
        } catch (error) {
            this.showStatus('Error: ' + error.message, false);
        }
    }

    async stop() {
        try {
            const response = await fetch('/stop', { method: 'POST' });
            const text = await response.text();
            if (response.ok) {
                this.isRunning = false;
                this.updateToggleButton();
                this.stopProgressPolling(); // Stop progress updates
                this.hideProgress(); // Hide progress display
                // Wait a bit before polling to allow server to update
                setTimeout(() => this.getQueue(), 1000);
            }
            this.showStatus(text, response.ok);
        } catch (error) {
            this.showStatus('Error: ' + error.message, false);
        }
    }

    async getQueue() {
        try {
            const response = await fetch('/queue');
            if (response.ok) {
                const data = await response.json();
                // Sync button state with server status
                this.isRunning = data.status.running;
                this.updateToggleButton();
                this.showQueue(data);
                this.showError(data.status.error);

                // Show/hide progress based on streaming status
                if (data.status.running && data.status.activelyStreaming) {
                    if (!this.progressInterval) {
                        this.startProgressPolling();
                    }
                } else {
                    this.stopProgressPolling();
                    this.hideProgress();
                }
            } else {
                const text = await response.text();
                this.showStatus(text, false);
            }
        } catch (error) {
            this.showStatus('Error: ' + error.message, false);
        }
    }

    async dequeue(id) {
        try {
            const response = await fetch(`/dequeue/${id}`, { method: 'DELETE' });
            const text = await response.text();
            this.showStatus(text, response.ok);

            if (response.ok) {
                // Refresh the queue after successful removal
                this.getQueue();
            }
        } catch (error) {
            this.showStatus('Error: ' + error.message, false);
        }
    }

    showStatus(message, isSuccess) {
        const statusDiv = document.getElementById('status');
        statusDiv.textContent = message;
        statusDiv.className = `mb-6 p-4 rounded-lg ${isSuccess ? 'bg-green-50 dark:bg-green-900/20 text-green-800 dark:text-green-200 border border-green-200 dark:border-green-800' : 'bg-red-50 dark:bg-red-900/20 text-red-800 dark:text-red-200 border border-red-200 dark:border-red-800'}`;
        statusDiv.classList.remove('hidden');

        setTimeout(() => {
            statusDiv.classList.add('hidden');
        }, 5000);
    }

    showQueue(data) {
        const queueDiv = document.getElementById('queue');
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

        // Add event listeners to remove buttons
        queueDiv.querySelectorAll('.remove-btn').forEach(btn => {
            btn.addEventListener('click', (e) => {
                const id = e.target.getAttribute('data-id');
                this.dequeue(id);
            });
        });
    }

    showError(error) {
        const errorDiv = document.getElementById('errorNotification');

        if (error) {
            const errorTime = new Date(error.time * 1000).toLocaleTimeString();
            errorDiv.innerHTML = `
                <div class="bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 text-red-800 dark:text-red-200 px-4 py-3 rounded-lg">
                    <div class="font-semibold">⚠️ FFmpeg Error (${errorTime})</div>
                    <div class="mt-1 text-sm">${error.message}</div>
                </div>
            `;
            errorDiv.classList.remove('hidden');
        } else {
            errorDiv.classList.add('hidden');
        }
    }

    startProgressPolling() {
        // Don't start multiple intervals
        if (this.progressInterval) {
            clearInterval(this.progressInterval);
        }

        // Poll progress every 2 seconds
        this.progressInterval = setInterval(() => this.getProgress(), 2000);

        // Get initial progress
        this.getProgress();
    }

    stopProgressPolling() {
        if (this.progressInterval) {
            clearInterval(this.progressInterval);
            this.progressInterval = null;
        }
    }

    async getProgress() {
        try {
            const response = await fetch('/progress');
            if (response.ok) {
                const data = await response.json();
                if (data.hasProgress && data.progress) {
                    this.showProgress(data.progress);
                } else if (!this.isRunning) {
                    this.hideProgress();
                }
            }
        } catch (error) {
            // Don't show errors for progress polling to avoid spam
            console.error('Progress polling error:', error);
        }
    }

    showProgress(progress) {
        const progressDiv = document.getElementById('progress');
        progressDiv.classList.remove('hidden');

        // Update progress data
        document.getElementById('progressFrame').textContent = progress.frame?.toLocaleString() || '-';
        document.getElementById('progressFps').textContent = progress.fps ? progress.fps.toFixed(1) : '-';
        document.getElementById('progressBitrate').textContent = progress.bitrate || '-';
        document.getElementById('progressTime').textContent = progress.out_time || '-';
        document.getElementById('progressSpeed').textContent = progress.speed || '-';

        // Update timestamp
        if (progress.timestamp) {
            const timestamp = new Date(progress.timestamp);
            document.getElementById('progressTimestamp').textContent = timestamp.toLocaleTimeString();
            this.lastProgressTime = timestamp;
        }

        // Calculate and show progress bar (estimate based on frame count)
        // This is a rough estimation - in a real scenario you'd need total duration
        if (progress.frame && progress.fps) {
            const estimatedTotalFrames = progress.fps * 300; // Assume 5 min video max for demo
            const percentage = Math.min((progress.frame / estimatedTotalFrames) * 100, 100);

            const progressBar = document.getElementById('progressBar');
            const progressPercentage = document.getElementById('progressPercentage');

            progressBar.style.width = percentage.toFixed(1) + '%';
            progressPercentage.textContent = percentage.toFixed(1) + '%';
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

    async loadServerFiles() {
        const fileBrowser = document.getElementById('fileBrowser');
        fileBrowser.innerHTML = '<div class="text-center text-gray-500 py-4">Loading...</div>';

        try {
            const url = `/files${this.currentPath !== '.' ? '?path=' + encodeURIComponent(this.currentPath) : ''}`;
            const response = await fetch(url);

            if (response.ok) {
                const data = await response.json();
                this.displayServerFiles(data.files, data.path);
                document.getElementById('currentPath').textContent = `📂 ${data.path === '.' ? '/' : data.path}`;
            } else {
                const text = await response.text();
                fileBrowser.innerHTML = `<div class="text-center text-red-500 py-4">Error: ${text}</div>`;
            }
        } catch (error) {
            fileBrowser.innerHTML = `<div class="text-center text-red-500 py-4">Error: ${error.message}</div>`;
        }
    }

    displayServerFiles(files, currentPath) {
        const fileBrowser = document.getElementById('fileBrowser');

        if (files.length === 0) {
            fileBrowser.innerHTML = '<div class="text-center text-gray-500 py-4">No video files found</div>';
            return;
        }

        let html = '';

        // Add parent directory link if not at root
        if (currentPath !== '.' && currentPath !== '/') {
            const parentPath = currentPath.split('/').slice(0, -1).join('/') || '.';
            html += `
                <div class="flex items-center p-2 hover:bg-gray-100 cursor-pointer border-b file-entry" data-path="${parentPath}" data-type="parent">
                    <span class="mr-2">📁</span>
                    <span class="text-blue-600">..</span>
                </div>
            `;
        }

        // Sort files: directories first, then files
        const sortedFiles = [...files].sort((a, b) => {
            if (a.isDir && !b.isDir) return -1;
            if (!a.isDir && b.isDir) return 1;
            return a.name.localeCompare(b.name);
        });

        sortedFiles.forEach(file => {
            const icon = file.isDir ? '📁' : '🎬';
            const sizeText = file.isDir ? '' : ` (${this.formatFileSize(file.size)})`;
            const pathClass = file.isDir ? 'text-primary-600 dark:text-primary-400' : 'text-gray-700 dark:text-gray-300';

            html += `
                <div class="flex items-center justify-between p-3 hover:bg-gray-50 dark:hover:bg-gray-600 cursor-pointer border-b border-gray-200 dark:border-gray-600 file-entry" data-path="${file.path}" data-type="${file.isDir ? 'dir' : 'file'}">
                    <div class="flex items-center flex-1">
                        <span class="mr-3 text-lg">${icon}</span>
                        <div class="flex-1">
                            <span class="${pathClass} font-medium">${file.name}</span>
                            <span class="text-xs text-gray-500 dark:text-gray-400 ml-2">${sizeText}</span>
                        </div>
                    </div>
                    ${!file.isDir ? '<div class="text-xs text-gray-400 dark:text-gray-500">' + new Date(file.modTime).toLocaleDateString() + '</div>' : ''}
                </div>
            `;
        });

        fileBrowser.innerHTML = html;

        // Add click event listeners
        fileBrowser.querySelectorAll('.file-entry').forEach(entry => {
            entry.addEventListener('click', (e) => {
                const path = e.currentTarget.getAttribute('data-path');
                const type = e.currentTarget.getAttribute('data-type');

                if (type === 'dir' || type === 'parent') {
                    this.currentPath = path;
                    this.loadServerFiles();
                } else if (type === 'file') {
                    this.selectServerFile(path);
                }
            });
        });
    }

    selectServerFile(filePath) {
        // Clear previous selection
        document.querySelectorAll('.file-entry').forEach(entry => {
            entry.classList.remove('bg-primary-50', 'dark:bg-primary-900/20');
        });

        // Highlight selected file
        const selectedEntry = document.querySelector(`[data-path="${filePath}"][data-type="file"]`);
        if (selectedEntry) {
            selectedEntry.classList.add('bg-primary-50', 'dark:bg-primary-900/20');
        }

        this.selectedFile = filePath;
        this.updateServerFileSelection();
    }

    updateServerFileSelection() {
        const enqueueBtn = document.getElementById('enqueueServerBtn');
        if (this.selectedFile) {
            enqueueBtn.disabled = false;
            enqueueBtn.textContent = `Add: ${this.selectedFile.split('/').pop()}`;
        } else {
            enqueueBtn.disabled = true;
            enqueueBtn.textContent = 'Add Selected File to Queue';
        }
    }

    formatFileSize(bytes) {
        if (bytes === 0) return '0 Bytes';
        const k = 1024;
        const sizes = ['Bytes', 'KB', 'MB', 'GB'];
        const i = Math.floor(Math.log(bytes) / Math.log(k));
        return parseFloat((bytes / Math.pow(k, i)).toFixed(2)) + ' ' + sizes[i];
    }

    // WebRTC Methods
    async connectWHEP() {
        if (this.isWhepConnected) {
            return;
        }

        try {
            this.updateWebRTCStatus('Connecting...', 'text-yellow-600');

            // Check if there's an active broadcast first
            const statusResponse = await fetch('/webrtc/status');
            const status = await statusResponse.json();

            if (!status.broadcasting && !status.has_video && !status.has_audio) {
                this.updateWebRTCStatus('No active broadcast available', 'text-red-600');
                this.showStatus('No active WebRTC broadcast. Start streaming first.', false);
                return;
            }

            // Create RTCPeerConnection
            this.whepConnection = new RTCPeerConnection({
                iceServers: [
                    { urls: 'stun:stun.l.google.com:19302' }
                ]
            });

            // Handle incoming tracks
            this.whepConnection.ontrack = (event) => {
                console.log('Received track:', event.track.kind);
                const video = document.getElementById('whepVideo');
                if (video.srcObject !== event.streams[0]) {
                    video.srcObject = event.streams[0];
                    document.getElementById('noStreamMessage').style.display = 'none';
                }
            };

            // Handle connection state changes
            this.whepConnection.onconnectionstatechange = () => {
                const state = this.whepConnection.connectionState;
                console.log('Connection state:', state);
                this.updateConnectionStats();

                if (state === 'connected') {
                    this.isWhepConnected = true;
                    this.updateWebRTCStatus('Connected', 'text-green-600');
                    this.showWebRTCButtons(false, true);
                    document.getElementById('webrtcStats').classList.remove('hidden');
                } else if (state === 'failed' || state === 'disconnected') {
                    this.disconnectWHEP();
                }
            };

            // Handle ICE connection state changes
            this.whepConnection.oniceconnectionstatechange = () => {
                console.log('ICE connection state:', this.whepConnection.iceConnectionState);
                this.updateConnectionStats();
            };

            // Create offer
            const offer = await this.whepConnection.createOffer();
            await this.whepConnection.setLocalDescription(offer);

            // Wait for ICE gathering to complete
            await this.waitForICEGathering();

            // Send offer to WHEP endpoint
            const response = await fetch('/whep', {
                method: 'POST',
                headers: {
                    'Content-Type': 'application/sdp'
                },
                body: this.whepConnection.localDescription.sdp
            });

            if (!response.ok) {
                const errorText = await response.text();
                throw new Error(`WHEP request failed: ${response.status} - ${errorText}`);
            }

            const answerSDP = await response.text();
            await this.whepConnection.setRemoteDescription({
                type: 'answer',
                sdp: answerSDP
            });

            this.updateWebRTCStatus('Connecting (ICE)...', 'text-yellow-600');

        } catch (error) {
            console.error('WHEP connection error:', error);
            this.updateWebRTCStatus('Connection failed', 'text-red-600');
            this.showStatus(`WebRTC connection failed: ${error.message}`, false);
            this.disconnectWHEP();
        }
    }

    disconnectWHEP() {
        if (this.whepConnection) {
            this.whepConnection.close();
            this.whepConnection = null;
        }

        this.isWhepConnected = false;
        const video = document.getElementById('whepVideo');
        video.srcObject = null;

        document.getElementById('noStreamMessage').style.display = 'flex';
        document.getElementById('webrtcStats').classList.add('hidden');

        this.updateWebRTCStatus('Not connected', 'text-gray-600');
        this.showWebRTCButtons(true, false);
    }

    waitForICEGathering() {
        return new Promise(resolve => {
            if (this.whepConnection.iceGatheringState === 'complete') {
                resolve();
            } else {
                this.whepConnection.addEventListener('icegatheringstatechange', () => {
                    if (this.whepConnection.iceGatheringState === 'complete') {
                        resolve();
                    }
                });
            }
        });
    }

    updateWebRTCStatus(message, className = 'text-gray-600') {
        const statusElement = document.getElementById('webrtcStatus');
        statusElement.textContent = message;
        statusElement.className = `text-sm ${className}`;
    }

    showWebRTCButtons(showConnect, showDisconnect) {
        const connectBtn = document.getElementById('whepConnect');
        const disconnectBtn = document.getElementById('whepDisconnect');

        if (showConnect) {
            connectBtn.classList.remove('hidden');
        } else {
            connectBtn.classList.add('hidden');
        }

        if (showDisconnect) {
            disconnectBtn.classList.remove('hidden');
        } else {
            disconnectBtn.classList.add('hidden');
        }
    }

    updateConnectionStats() {
        if (!this.whepConnection) return;

        document.getElementById('connectionState').textContent = this.whepConnection.connectionState;
        document.getElementById('iceState').textContent = this.whepConnection.iceConnectionState;
    }

    startWebRTCStatusPolling() {
        // Poll WebRTC status every 10 seconds
        this.webrtcStatusInterval = setInterval(() => this.checkWebRTCStatus(), 10000);

        // Check initial status
        this.checkWebRTCStatus();
    }

    async checkWebRTCStatus() {
        try {
            const response = await fetch('/webrtc/status');
            if (response.ok) {
                const status = await response.json();
                // You could update UI based on server status here
                // For example, auto-disconnect if server stops broadcasting
                if (this.isWhepConnected && !status.broadcasting) {
                    console.log('Server stopped broadcasting, disconnecting...');
                    this.disconnectWHEP();
                }
            }
        } catch (error) {
            console.error('WebRTC status check error:', error);
        }
    }

    // Subtitle browser methods
    handleSubtitleBrowse(mode) {
        if (mode === 'local') {
            // For local files, trigger the hidden file input
            const subtitleFileInput = document.getElementById('subtitleFileInput');
            subtitleFileInput.onchange = (e) => {
                if (e.target.files.length > 0) {
                    const fileName = e.target.files[0].name;
                    document.getElementById('subtitleFile').value = fileName;
                    // Store the file for potential upload later
                    this.selectedLocalSubtitleFile = e.target.files[0];
                }
            };
            subtitleFileInput.click();
        } else {
            // For server files, open the browser modal
            this.openSubtitleBrowser('subtitleFileServer');
        }
    }

    openSubtitleBrowser(targetInputId) {
        this.subtitleTargetInput = targetInputId;
        this.subtitleCurrentPath = '.';
        this.selectedSubtitleFile = null;

        const modal = document.getElementById('subtitleBrowserModal');
        modal.classList.remove('hidden');

        this.loadSubtitleFiles();
        this.updateSubtitleSelection();
    }

    closeSubtitleBrowser() {
        const modal = document.getElementById('subtitleBrowserModal');
        modal.classList.add('hidden');

        this.subtitleTargetInput = null;
        this.selectedSubtitleFile = null;
    }

    async loadSubtitleFiles() {
        const fileBrowser = document.getElementById('subtitleFileBrowser');
        fileBrowser.innerHTML = '<div class="text-center text-gray-500 py-4">Loading...</div>';

        try {
            const url = `/files${this.subtitleCurrentPath !== '.' ? '?path=' + encodeURIComponent(this.subtitleCurrentPath) : ''}`;
            const response = await fetch(url);

            if (response.ok) {
                const data = await response.json();
                this.displaySubtitleFiles(data.files, data.path);
                document.getElementById('subtitleCurrentPath').textContent = `📂 ${data.path === '.' ? '/' : data.path}`;
            } else {
                const text = await response.text();
                fileBrowser.innerHTML = `<div class="text-center text-red-500 py-4">Error: ${text}</div>`;
            }
        } catch (error) {
            fileBrowser.innerHTML = `<div class="text-center text-red-500 py-4">Error: ${error.message}</div>`;
        }
    }

    displaySubtitleFiles(files, currentPath) {
        const fileBrowser = document.getElementById('subtitleFileBrowser');

        // Filter for subtitle files and directories only
        const subtitleFiles = files.filter(file => file.isDir || this.isSubtitleFile(file.name));

        if (subtitleFiles.length === 0) {
            fileBrowser.innerHTML = '<div class="text-center text-gray-500 py-4">No subtitle files found</div>';
            return;
        }

        let html = '';

        // Add parent directory link if not at root
        if (currentPath !== '.' && currentPath !== '/') {
            const parentPath = currentPath.split('/').slice(0, -1).join('/') || '.';
            html += `
                <div class="flex items-center p-2 hover:bg-gray-100 cursor-pointer border-b subtitle-entry" data-path="${parentPath}" data-type="parent">
                    <span class="mr-2">📁</span>
                    <span class="text-blue-600">..</span>
                </div>
            `;
        }

        // Sort files: directories first, then files
        const sortedFiles = [...subtitleFiles].sort((a, b) => {
            if (a.isDir && !b.isDir) return -1;
            if (!a.isDir && b.isDir) return 1;
            return a.name.localeCompare(b.name);
        });

        sortedFiles.forEach(file => {
            const icon = file.isDir ? '📁' : '📄';
            const sizeText = file.isDir ? '' : ` (${this.formatFileSize(file.size)})`;
            const pathClass = file.isDir ? 'text-primary-600 dark:text-primary-400' : 'text-gray-700 dark:text-gray-300';

            html += `
                <div class="flex items-center justify-between p-3 hover:bg-gray-50 dark:hover:bg-gray-600 cursor-pointer border-b border-gray-200 dark:border-gray-600 subtitle-entry" data-path="${file.path}" data-type="${file.isDir ? 'dir' : 'file'}">
                    <div class="flex items-center flex-1">
                        <span class="mr-3 text-lg">${icon}</span>
                        <div class="flex-1">
                            <span class="${pathClass} font-medium">${file.name}</span>
                            <span class="text-xs text-gray-500 dark:text-gray-400 ml-2">${sizeText}</span>
                        </div>
                    </div>
                    ${!file.isDir ? '<div class="text-xs text-gray-400 dark:text-gray-500">' + new Date(file.modTime).toLocaleDateString() + '</div>' : ''}
                </div>
            `;
        });

        fileBrowser.innerHTML = html;

        // Add click event listeners
        fileBrowser.querySelectorAll('.subtitle-entry').forEach(entry => {
            entry.addEventListener('click', (e) => {
                const path = e.currentTarget.getAttribute('data-path');
                const type = e.currentTarget.getAttribute('data-type');

                if (type === 'dir' || type === 'parent') {
                    this.subtitleCurrentPath = path;
                    this.loadSubtitleFiles();
                } else if (type === 'file') {
                    this.selectSubtitleFileFromBrowser(path);
                }
            });
        });
    }

    selectSubtitleFileFromBrowser(filePath) {
        // Clear previous selection
        document.querySelectorAll('.subtitle-entry').forEach(entry => {
            entry.classList.remove('bg-primary-50', 'dark:bg-primary-900/20');
        });

        // Highlight selected file
        const selectedEntry = document.querySelector(`[data-path="${filePath}"][data-type="file"]`);
        if (selectedEntry) {
            selectedEntry.classList.add('bg-primary-50', 'dark:bg-primary-900/20');
        }

        this.selectedSubtitleFile = filePath;
        this.updateSubtitleSelection();
    }

    updateSubtitleSelection() {
        const selectBtn = document.getElementById('selectSubtitleBtn');
        if (this.selectedSubtitleFile) {
            selectBtn.disabled = false;
            selectBtn.textContent = `Select: ${this.selectedSubtitleFile.split('/').pop()}`;
        } else {
            selectBtn.disabled = true;
            selectBtn.textContent = 'Select File';
        }
    }

    selectSubtitleFile() {
        if (this.selectedSubtitleFile && this.subtitleTargetInput) {
            const input = document.getElementById(this.subtitleTargetInput);
            if (input) {
                input.value = this.selectedSubtitleFile;
            }
        }
        this.closeSubtitleBrowser();
    }

    refreshSubtitleFiles() {
        this.loadSubtitleFiles();
    }

    isSubtitleFile(filename) {
        const ext = filename.toLowerCase().split('.').pop();
        const subtitleExtensions = ['srt', 'vtt', 'ass', 'ssa', 'sub', 'sbv'];
        return subtitleExtensions.includes(ext);
    }
}

document.addEventListener('DOMContentLoaded', () => {
    new StreamManager();
});
