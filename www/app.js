class StreamManager {
    constructor() {
        this.isRunning = false;
        this.player = null;
        this.initEventListeners();
    }

    initEventListeners() {
        document.getElementById('toggleBtn').addEventListener('click', () => this.toggle());
        document.getElementById('enqueueBtn').addEventListener('click', () => this.enqueue());
        document.getElementById('skipBtn').addEventListener('click', () => this.skip());
        document.getElementById('configToggle').addEventListener('click', () => this.toggleConfigSettings());
        document.getElementById('overlayToggle').addEventListener('click', () => this.toggleOverlaySettings());
        document.getElementById('showFilename').addEventListener('change', () => this.toggleOverlayOptions());

        // Auto-refresh queue on page load
        this.getQueue();

        // Set initial overlay options visibility
        this.toggleOverlayOptions();

        // Auto-refresh queue every 5 seconds
        setInterval(() => this.getQueue(), 5000);
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
            options.style.display = 'grid';
        } else {
            options.style.display = 'none';
        }
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
            }
            this.showStatus(text, response.ok);
        } catch (error) {
            this.showStatus('Error: ' + error.message, false);
        }
    }

    async enqueue() {
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

        try {
            const response = await fetch('/enqueue', {
                method: 'POST',
                headers: {
                    'Content-Type': 'application/json'
                },
                body: JSON.stringify({
                    file: fileName,
                    overlay: overlaySettings
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
        statusDiv.className = `mt-4 p-3 rounded text-sm ${isSuccess ? 'bg-green-100 text-green-800' : 'bg-red-100 text-red-800'}`;
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
        if (status.running) {
            statusText = status.activelyStreaming ? 'Streaming' : 'Ready';
        }
        let html = `<strong>Status:</strong> ${statusText}<br>`;
        if (status.playing) {
            html += `<strong>Currently Playing:</strong> ${status.playing.file}<br>`;
        }

        html += `<br><strong>Queue (${queueItems.length} items):</strong><br>`;

        if (queueItems.length === 0) {
            html += 'No items in queue';
        } else {
            queueItems.forEach((item, index) => {
                html += `
                    <div class="flex justify-between items-center mb-1 p-2 bg-white rounded border">
                        <span class="text-sm">${index + 1}. ${item.file}</span>
                        <button class="remove-btn bg-red-500 text-white px-2 py-1 rounded text-xs hover:bg-red-600" data-id="${item.id}">
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
                <div class="bg-red-100 border border-red-400 text-red-700 px-4 py-3 rounded">
                    <strong>⚠️ FFmpeg Error (${errorTime}):</strong><br>
                    ${error.message}
                </div>
            `;
            errorDiv.classList.remove('hidden');
        } else {
            errorDiv.classList.add('hidden');
        }
    }
}

document.addEventListener('DOMContentLoaded', () => {
    new StreamManager();
});
