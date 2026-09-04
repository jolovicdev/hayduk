import { serviceOpen } from "./serviceState";

export interface LoginModule {
  module: string;
  label: string;
  userKey: string;
  passKey: string;
  ports: number[];
  names: string[];
}

export const LOGIN_MODULES: LoginModule[] = [
  {
    module: "scanner/smb/smb_login", label: "SMB", userKey: "SMBUser", passKey: "SMBPass",
    ports: [139, 445], names: ["smb", "microsoft-ds", "netbios-ssn"],
  },
  {
    module: "scanner/ssh/ssh_login", label: "SSH", userKey: "USERNAME", passKey: "PASSWORD",
    ports: [22], names: ["ssh"],
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
