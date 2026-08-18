import { LockKeyhole, LogOut } from 'lucide-react'

export function DeviceSessionAction({ allowSelfUnpair, onLogout }: { allowSelfUnpair: boolean; onLogout: () => void }) {
  if (!allowSelfUnpair) return <p className="locked-setting"><LockKeyhole /> A parent controls device pairing.</p>
  return <button onClick={onLogout}><LogOut /> Sign out this device</button>
}
