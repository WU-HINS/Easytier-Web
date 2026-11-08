import { useCopy } from '../../copy/locale';
import { BlockedDevices } from './blocked-devices';

/** Blocked devices on their own page, the way the first console listed them. */
export function BlockedPage() {
  const copy = useCopy();
  return (
    <div className="space-y-[var(--gap-section)]">
      <header>
        <h1 className="text-phi-3xl font-semibold tracking-tight">{copy.nav.blocked}</h1>
      </header>
      <BlockedDevices />
    </div>
  );
}
