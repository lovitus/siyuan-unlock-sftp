// SFTP owns its form and persistence. Only provider selection and mounting touch upstream.
import {fetchSyncPost} from "../../util/fetch";

export interface SFTPConfig {
    host: string; port: number; username: string; password: string;
    path: string; hostKey: string; timeout: number; concurrentReqs: number;
}
const fields: Array<[keyof SFTPConfig, string, string, number?, number?]> = [
    ["host", "Host", "text"], ["port", "Port", "number", 1, 65535],
    ["username", "Username", "text"], ["password", "Password", "password"],
    ["path", "Path (absolute, required)", "text"], ["hostKey", "Host key (SHA256)", "text"],
    ["timeout", "Timeout (s)", "number", 7, 300], ["concurrentReqs", "Concurrent requests", "number", 1, 16],
];
const defaults: SFTPConfig = {host: "", port: 22, username: "", password: "", path: "", hostKey: "", timeout: 30, concurrentReqs: 4};

export function renderSFTPConfig(root: Element): boolean {
    const sync = window.siyuan.config.sync;
    if (sync.provider !== 5) { return false; }
    const panel = root.querySelector<HTMLElement>("#syncProviderConfig");
    if (!panel) { return true; }
    root.querySelector("#syncProviderConfigRow")?.classList.add("fn__none");
    panel.replaceChildren();
    const title = document.createElement("div");
    title.className = "b3-label__text";
    title.textContent = "SFTP — specify an existing absolute remote path and the server's SHA256 host key fingerprint.";
    panel.append(title);
    const form = document.createElement("form");
    form.autocomplete = "off";
    const config = {...defaults, ...sync.sftp};
    const readonly = window.siyuan.config.readonly;
    for (const [key, label, type, min, max] of fields) {
        const row = document.createElement("label");
        row.className = "b3-label b3-label--inner fn__flex";
        row.append(document.createTextNode(label));
        const input = document.createElement("input");
        input.name = key;
        input.type = type;
        input.className = "b3-text-field fn__flex-1";
        input.value = String(config[key]);
        input.disabled = readonly;
        input.required = key !== "password";
        if (min !== undefined) { input.min = String(min); input.max = String(max); input.step = "1"; }
        if (key === "path") { input.placeholder = "/srv/siyuan"; }
        if (key === "hostKey") { input.placeholder = "SHA256:..."; }
        row.append(input);
        form.append(row);
    }
    const save = document.createElement("button");
    save.type = "submit";
    save.className = "b3-button";
    save.textContent = window.siyuan.languages.save || "Save";
    save.disabled = readonly;
    const status = document.createElement("div");
    status.setAttribute("role", "status");
    form.append(save, status);
    panel.append(form);
    let pending = false;
    form.addEventListener("submit", async event => {
        event.preventDefault();
        if (pending || window.siyuan.config.readonly || window.siyuan.config.sync.provider !== 5 || !form.reportValidity()) { return; }
        const values = Object.fromEntries(fields.map(([key, ,type]) => {
            const raw = form.querySelector<HTMLInputElement>(`[name="${key}"]`).value;
            return [key, type === "number" ? Number(raw) : key === "password" ? raw : raw.trim()];
        })) as unknown as SFTPConfig;
        if (!values.path.startsWith("/")) { status.textContent = "SFTP path must be absolute"; return; }
        if (!/^SHA256:[A-Za-z0-9+/]{43}$/.test(values.hostKey)) { status.textContent = "A valid SHA256 host key fingerprint is required"; return; }
        pending = true; save.disabled = true; status.textContent = "";
        try {
            // This extension route is not in upstream's generated route union.
            const post = fetchSyncPost as (url: string, data: unknown) => Promise<{code: number; msg?: string; data?: {sftp: SFTPConfig}}>;
            const response = await post("/api/sync/setSyncProviderSFTP", {sftp: values});
            if (response.code !== 0 || !response.data?.sftp) { throw new Error(response.msg || "SFTP configuration was not saved"); }
            window.siyuan.config.sync.sftp = response.data.sftp;
            status.textContent = "Saved";
        } catch (error) {
            status.textContent = error instanceof Error ? error.message : String(error);
        } finally { pending = false; save.disabled = !!window.siyuan.config.readonly; }
    });
    return true;
}
