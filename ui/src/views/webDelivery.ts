// Web delivery platforms map onto exploit/multi/script/web_delivery targets.
// The engine captures the one-liner the module prints, so the UI only picks
// the platform, a compatible default payload, and the callback addresses.

export interface WebDeliveryPlatform {
  value: string;
  label: string;
  payload: string;
}

export const WEB_DELIVERY_PLATFORMS: WebDeliveryPlatform[] = [
  { value: "python", label: "Python", payload: "python/meterpreter/reverse_tcp" },
  { value: "php", label: "PHP", payload: "php/meterpreter/reverse_tcp" },
  { value: "psh", label: "PowerShell", payload: "windows/x64/meterpreter/reverse_tcp" },
  { value: "regsvr32", label: "Regsvr32 (AppLocker bypass)", payload: "windows/x64/meterpreter/reverse_tcp" },
  { value: "linux", label: "Linux (wget)", payload: "linux/x64/meterpreter/reverse_tcp" },
  { value: "osx", label: "macOS (curl)", payload: "osx/x64/meterpreter/reverse_tcp" },
];

export function defaultPlatform(value: string): WebDeliveryPlatform {
  return WEB_DELIVERY_PLATFORMS.find(p => p.value === value) ?? WEB_DELIVERY_PLATFORMS[0]!;
}

export function webDeliveryReady(o: {
  payload: string;
  lhost: string;
  lport: string;
  srvPort: string;
}): boolean {
  return o.payload !== "" && o.lhost.trim() !== "" &&
    Number(o.lport) > 0 && Number(o.srvPort) > 0;
}
