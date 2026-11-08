import { useCopy } from '../copy/locale';
import { Button } from './button';

/**
 * The third state every query has.
 *
 * A page that only knows loading and empty will render "you have nothing" when the
 * request actually failed, which is the interface telling a lie the operator acts on
 * (review round r259, R-D). This renders the failure with a way out, so empty states
 * stay reserved for a successful empty answer.
 */
export function QueryError({ onRetry }: { onRetry: () => void }) {
  const copy = useCopy();
  return (
    <div
      role="alert"
      className="flex flex-wrap items-center justify-between gap-[var(--u-4)] rounded-[calc(var(--radius)*0.8)] border border-destructive/40 bg-destructive/5 px-[var(--u-6)] py-[var(--u-4)] text-phi-sm text-destructive"
    >
      <span>{copy.common.loadFailed}</span>
      <Button variant="secondary" size="sm" onClick={onRetry}>
        {copy.common.retry}
      </Button>
    </div>
  );
}
