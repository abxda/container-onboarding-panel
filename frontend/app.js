// Frontend del launcher de contenedor (Edición Container).
// Habla con el backend Go via window.go.main.App.* y escucha eventos
// (log, download:progress, shutdown:*) via window.runtime.EventsOn.

const App = () => window.go.main.App;
const $ = (id) => document.getElementById(id);

let env = null;          // EnvInfo
let snap = null;         // último Snapshot
let busy = false;        // bloquea acciones concurrentes
let pollTimer = null;

// ---------- utilidades ----------

function logline(level, msg) {
    const box = $("log");
    const el = document.createElement("div");
    el.className = "logline";
    el.innerHTML = `<span class="lv lv-${level}">${level}</span><span class="msg"></span>`;
    el.querySelector(".msg").textContent = msg;
    box.appendChild(el);
    box.scrollTop = box.scrollHeight;
}

function setOverlay(on, txt) {
    $("overlayTxt").textContent = txt || "Trabajando…";
    $("overlay").classList.toggle("hidden", !on);
}

async function withBusy(txt, fn) {
    if (busy) return;
    busy = true;
    setOverlay(true, txt);
    try {
        await fn();
    } catch (e) {
        logline("ERROR", String(e));
    } finally {
        busy = false;
        setOverlay(false);
        await refresh();
    }
}

// ---------- render ----------

function renderEnv() {
    if (!env) return;
    const ver = env.version ? `Podman ${env.version}` : "Podman no detectado";
    $("env").textContent = `${env.os}/${env.arch} · ${ver}`;
    $("podmanHint").textContent = env.installHint;
    // En Linux no hay "máquina": ese paso no aplica.
    if (!env.needsMachine) {
        $("step-machine").classList.add("step--na");
        $("machineHint").textContent = "Linux: Podman corre nativo, no necesita motor extra.";
        $("btnMachine").classList.add("hidden");
    }
}

function stepClass(el, state) {
    el.classList.remove("step--done", "step--active");
    if (state === "done") el.classList.add("step--done");
    else if (state === "active") el.classList.add("step--active");
}

function render() {
    if (!snap) return;

    // --- Paso 1: Podman ---
    const podmanDone = snap.podmanOk;
    stepClass($("step-podman"), podmanDone ? "done" : "active");
    $("btnInstall").classList.toggle("hidden", podmanDone);

    // --- Paso 2: motor/máquina ---
    const needsMachine = env && env.needsMachine;
    let machineDone = true;
    if (needsMachine) {
        machineDone = snap.machineRunning;
        stepClass($("step-machine"), !podmanDone ? "" : (machineDone ? "done" : "active"));
        $("btnMachine").disabled = !podmanDone;
        $("btnMachine").classList.toggle("hidden", machineDone);
    }

    // --- Paso 3: imagen ---
    const imgDone = snap.imageLoaded;
    const imgReady = podmanDone && machineDone;
    stepClass($("step-image"), !imgReady ? "" : (imgDone ? "done" : "active"));
    $("btnImage").disabled = !imgReady;
    $("btnImage").classList.toggle("hidden", imgDone);

    // --- Capa 2: laboratorio ---
    const canStart = podmanDone && machineDone && imgDone;
    const running = snap.running;
    const pill = $("labPill");
    pill.className = "pill " + (running ? "pill--on" : "pill--off");
    pill.textContent = running ? "En ejecución" : "Detenido";

    $("btnStart").disabled = !canStart || running;
    $("btnStop").disabled = !running;

    const s = snap.services || {};
    $("btnJupyter").disabled = !s.jupyter;
    $("btnHDFS").disabled = !s.hdfs;
    $("svc-hdfs").classList.toggle("svc--up", !!s.hdfs);
    $("svc-elastic").classList.toggle("svc--up", !!s.elastic);
    $("svc-jupyter").classList.toggle("svc--up", !!s.jupyter);
}

// ---------- datos ----------

async function refresh() {
    try {
        snap = await App().Diagnose();
        render();
    } catch (e) {
        // backend aún no listo; reintenta en el siguiente poll
    }
}

// ---------- acciones ----------

const actions = {
    install: () => withBusy("Instalando Podman… (puede pedir permisos del sistema)", async () => {
        const r = await App().InstallPodman();
        if (!r.ok) logline("WARN", r.message);
    }),
    machine: () => withBusy("Preparando el motor de contenedores…", async () => {
        const r = await App().PrepareMachine();
        if (!r.ok) logline("ERROR", r.message);
    }),
    image: () => withBusy("Descargando y cargando la imagen del laboratorio…", async () => {
        $("dlProgress").classList.remove("hidden");
        const r = await App().DownloadImage();
        $("dlProgress").classList.add("hidden");
        if (!r.ok) logline("ERROR", r.message);
    }),
    start: () => withBusy("Arrancando el laboratorio…", async () => {
        const r = await App().StartLab();
        if (!r.ok) logline("ERROR", r.message);
    }),
    stop: () => withBusy("Deteniendo el laboratorio…", async () => {
        const r = await App().StopLab();
        if (!r.ok) logline("ERROR", r.message);
    }),
    jupyter: () => App().OpenJupyter(),
    hdfs: () => App().OpenHDFS(),
    folder: () => App().OpenWorkFolder(),
    smoke: () => withBusy("Descargando el cuaderno de prueba (TestGlobalBigData)…", async () => {
        const r = await App().DownloadSmokeTest();
        if (!r.ok) logline("ERROR", r.message);
    }),
};

function wire() {
    document.querySelectorAll("[data-act]").forEach((b) => {
        b.addEventListener("click", () => {
            const fn = actions[b.dataset.act];
            if (fn) fn();
        });
    });
}

// ---------- eventos del backend ----------

function listen() {
    const rt = window.runtime;
    rt.EventsOn("log", (e) => logline(e.level || "INFO", e.msg || ""));
    rt.EventsOn("download:progress", (p) => {
        $("dlBar").style.width = (p.pct || 0) + "%";
        $("dlTxt").textContent = `${p.pct || 0}% · ${p.doneMB || 0}/${p.totalMB || 0} MB`;
    });
    rt.EventsOn("shutdown:start", () => setOverlay(true, "Cerrando el laboratorio limpiamente…"));
    rt.EventsOn("shutdown:done", () => setOverlay(true, "Listo. Cerrando…"));
}

// ---------- arranque ----------

window.addEventListener("DOMContentLoaded", async () => {
    wire();
    listen();
    logline("INFO", "Edición Container · Podman. Te guiamos paso a paso desde cero.");
    try {
        env = await App().GetEnv();
        renderEnv();
        const ver = env.version ? `Podman ${env.version} detectado` : "Podman aún no detectado";
        logline("INFO", `Equipo: ${env.os}/${env.arch} · ${ver}.`);
    } catch (e) {
        logline("ERROR", "No pude leer el entorno: " + e);
    }
    await refresh();
    pollTimer = setInterval(refresh, 3500);
});
