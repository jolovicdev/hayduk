import { serviceOpen } from "./serviceState";

export interface LoginModule {
  module: string;
  label: string;
  // test modules validate credentials; session modules run an exploit that
  // pops a session (PsExec, sshexec, winrm_script_exec) with a chosen payload
  kind: "test" | "session";
  moduleType: "auxiliary" | "exploit";
  userKey: string;
  passKey: string;
  domainKey?: string;
  ports: number[];
  names: string[];
  payloads?: string[];
}

// SMBPass accepts an NTLM hash as-is, so the same fields carry Pass the Hash:
// paste the hash (or pick an ntlm cred) and psexec/smb_login use it directly.
export const LOGIN_MODULES: LoginModule[] = [
  {
    module: "scanner/smb/smb_login", label: "SMB · test credentials",
    kind: "test", moduleType: "auxiliary",
    userKey: "SMBUser", passKey: "SMBPass", domainKey: "SMBDomain",
    ports: [139, 445], names: ["smb", "microsoft-ds", "netbios-ssn"],
  },
  {
    module: "windows/smb/psexec", label: "SMB · PsExec session",
    kind: "session", moduleType: "exploit",
    userKey: "SMBUser", passKey: "SMBPass", domainKey: "SMBDomain",
    ports: [139, 445], names: ["smb", "microsoft-ds", "netbios-ssn"],
    payloads: [
      "windows/x64/meterpreter/reverse_tcp",
      "windows/meterpreter/reverse_tcp",
      "windows/x64/shell/reverse_tcp",
    ],
  },
  {
    module: "scanner/ssh/ssh_login", label: "SSH · test credentials",
    kind: "test", moduleType: "auxiliary",
    userKey: "USERNAME", passKey: "PASSWORD",
    ports: [22], names: ["ssh"],
  },
  {
    module: "multi/ssh/sshexec", label: "SSH · session",
    kind: "session", moduleType: "exploit",
    userKey: "USERNAME", passKey: "PASSWORD",
    ports: [22], names: ["ssh"],
    payloads: [
      "linux/x64/meterpreter/reverse_tcp",
      "linux/x86/meterpreter/reverse_tcp",
      "linux/x64/shell/reverse_tcp",
    ],
  },
  {
    module: "scanner/winrm/winrm_login", label: "WinRM · test credentials",
    kind: "test", moduleType: "auxiliary",
    userKey: "USERNAME", passKey: "PASSWORD",
    ports: [5985, 5986], names: ["winrm", "wsman"],
  },
  {
    module: "windows/winrm/winrm_script_exec", label: "WinRM · session",
    kind: "session", moduleType: "exploit",
    userKey: "USERNAME", passKey: "PASSWORD",
    ports: [5985, 5986], names: ["winrm", "wsman"],
    payloads: [
      "windows/x64/meterpreter/reverse_tcp",
      "windows/meterpreter/reverse_tcp",
    ],
  },
];

// Login modules worth offering for the services a host runs. A service
// matches on its database name or its port, and only while its port is
// actually reachable - a closed SMB row is not a login target. The
// operator can still pick any module in the dialog, this only preselects.
// The matched service's port rides along so logins can target
// non-standard ports.
export function loginOptions(
  services: readonly { port?: number; name: string; state?: string }[],
): (LoginModule & { port?: number })[] {
  const found: (LoginModule & { port?: number })[] = [];
  for (const lm of LOGIN_MODULES) {
    const hit = services.find(s =>
      serviceOpen(s) && (lm.names.some(n => s.name.includes(n)) || lm.ports.includes(s.port ?? -1)));
    if (hit) found.push({ ...lm, port: hit.port });
  }
  return found;
}

// Order recovered credentials: ones with both a user and a password first.
export function rankCreds(creds: readonly { user?: string; pass?: string }[]): { user?: string; pass?: string }[] {
  return [...creds].sort((a, b) => Number(!!b.user && !!b.pass) - Number(!!a.user && !!a.pass));
}

// looksLikeNtlmHash spots a bare NTLM hash or an LM:NTLM pair so the dialog
// can label the launch as Pass the Hash instead of a password login.
export function looksLikeNtlmHash(value: string): boolean {
  const v = value.trim();
  return /^[0-9a-fA-F]{32}$/.test(v) || /^[0-9a-fA-F]{32}:[0-9a-fA-F]{32}$/.test(v);
}
