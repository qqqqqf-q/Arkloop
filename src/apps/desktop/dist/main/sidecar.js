"use strict";
var __createBinding = (this && this.__createBinding) || (Object.create ? (function(o, m, k, k2) {
    if (k2 === undefined) k2 = k;
    var desc = Object.getOwnPropertyDescriptor(m, k);
    if (!desc || ("get" in desc ? !m.__esModule : desc.writable || desc.configurable)) {
      desc = { enumerable: true, get: function() { return m[k]; } };
    }
    Object.defineProperty(o, k2, desc);
}) : (function(o, m, k, k2) {
    if (k2 === undefined) k2 = k;
    o[k2] = m[k];
}));
var __setModuleDefault = (this && this.__setModuleDefault) || (Object.create ? (function(o, v) {
    Object.defineProperty(o, "default", { enumerable: true, value: v });
}) : function(o, v) {
    o["default"] = v;
});
var __importStar = (this && this.__importStar) || (function () {
    var ownKeys = function(o) {
        ownKeys = Object.getOwnPropertyNames || function (o) {
            var ar = [];
            for (var k in o) if (Object.prototype.hasOwnProperty.call(o, k)) ar[ar.length] = k;
            return ar;
        };
        return ownKeys(o);
    };
    return function (mod) {
        if (mod && mod.__esModule) return mod;
        var result = {};
        if (mod != null) for (var k = ownKeys(mod), i = 0; i < k.length; i++) if (k[i] !== "default") __createBinding(result, mod, k[i]);
        __setModuleDefault(result, mod);
        return result;
    };
})();
Object.defineProperty(exports, "__esModule", { value: true });
exports.getSidecarStatus = getSidecarStatus;
exports.setStatusListener = setStatusListener;
exports.startSidecar = startSidecar;
exports.stopSidecar = stopSidecar;
const child_process_1 = require("child_process");
const path = __importStar(require("path"));
const http = __importStar(require("http"));
const electron_1 = require("electron");
const HEALTH_POLL_MS = 500;
const HEALTH_TIMEOUT_MS = 30_000;
const MAX_RESTARTS = 3;
let proc = null;
let status = 'stopped';
let restartCount = 0;
let onStatusChange = null;
function getSidecarStatus() {
    return status;
}
function setStatusListener(fn) {
    onStatusChange = fn;
}
function setStatus(s) {
    status = s;
    onStatusChange?.(s);
}
function resolveBinaryPath() {
    const isPackaged = electron_1.app.isPackaged;
    if (isPackaged) {
        return path.join(process.resourcesPath, 'sidecar', 'desktop');
    }
    // dev: 从 Go 构建产物读取
    return path.resolve(__dirname, '..', '..', '..', '..', 'services', 'desktop', 'bin', 'desktop');
}
function healthCheck(port) {
    return new Promise((resolve) => {
        const req = http.get(`http://127.0.0.1:${port}/healthz`, (res) => {
            resolve(res.statusCode === 200);
        });
        req.on('error', () => resolve(false));
        req.setTimeout(2000, () => {
            req.destroy();
            resolve(false);
        });
    });
}
async function waitForHealthy(port) {
    const deadline = Date.now() + HEALTH_TIMEOUT_MS;
    while (Date.now() < deadline) {
        if (await healthCheck(port))
            return true;
        await new Promise((r) => setTimeout(r, HEALTH_POLL_MS));
    }
    return false;
}
async function startSidecar(port) {
    if (proc)
        return;
    const binPath = resolveBinaryPath();
    setStatus('starting');
    proc = (0, child_process_1.spawn)(binPath, [], {
        env: {
            ...process.env,
            ARKLOOP_API_GO_ADDR: `127.0.0.1:${port}`,
        },
        stdio: ['ignore', 'pipe', 'pipe'],
    });
    proc.stdout?.on('data', (chunk) => {
        process.stdout.write(`[sidecar] ${chunk.toString()}`);
    });
    proc.stderr?.on('data', (chunk) => {
        process.stderr.write(`[sidecar] ${chunk.toString()}`);
    });
    proc.on('exit', (code) => {
        proc = null;
        if (status === 'stopped')
            return;
        console.error(`sidecar exited: code=${code}`);
        if (restartCount < MAX_RESTARTS) {
            restartCount++;
            setStatus('crashed');
            setTimeout(() => startSidecar(port), 1000);
        }
        else {
            setStatus('crashed');
        }
    });
    const ok = await waitForHealthy(port);
    if (ok) {
        restartCount = 0;
        setStatus('running');
    }
    else {
        setStatus('crashed');
        stopSidecar();
    }
}
function stopSidecar() {
    return new Promise((resolve) => {
        if (!proc) {
            setStatus('stopped');
            resolve();
            return;
        }
        setStatus('stopped');
        const p = proc;
        proc = null;
        const killTimer = setTimeout(() => {
            try {
                p.kill('SIGKILL');
            }
            catch { }
            resolve();
        }, 5000);
        p.on('exit', () => {
            clearTimeout(killTimer);
            resolve();
        });
        try {
            p.kill('SIGTERM');
        }
        catch { }
    });
}
//# sourceMappingURL=sidecar.js.map