import { copy } from '../../copy/zh-CN';
import { BlockedDevices } from './blocked-devices';

/** Blocked devices on their own page, the way the first console listed them. */
export function BlockedPage() {
  return (
    <div className="space-y-[var(--gap-section)]">
      <header>
        <h1 className="text-phi-2xl font-semibold tracking-tight">{copy.nav.blocked}</h1>
      </header>
      <BlockedDevices />
    </div>
  );
}
