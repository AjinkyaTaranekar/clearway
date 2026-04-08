import { JourneyStatus } from '../../types';

const config: Record<
  JourneyStatus,
  { label: string; bg: string; text: string; dot: string }
> = {
  approved: {
    label: 'Approved',
    bg: 'var(--status-approved-bg)',
    text: 'var(--status-approved-text)',
    dot: '#1E6639',
  },
  pending: {
    label: 'Pending',
    bg: 'var(--status-pending-bg)',
    text: 'var(--status-pending-text)',
    dot: '#7A4500',
  },
  rejected: {
    label: 'Rejected',
    bg: 'var(--status-rejected-bg)',
    text: 'var(--status-rejected-text)',
    dot: '#8E1B13',
  },
  active: {
    label: 'Active',
    bg: 'var(--status-active-bg)',
    text: 'var(--status-active-text)',
    dot: '#1A4E80',
  },
  completed: {
    label: 'Completed',
    bg: 'var(--status-completed-bg)',
    text: 'var(--status-completed-text)',
    dot: '#444444',
  },
  cancelled: {
    label: 'Cancelled',
    bg: 'var(--status-cancelled-bg)',
    text: 'var(--status-cancelled-text)',
    dot: '#6B4848',
  },
};

interface StatusChipProps {
  status: JourneyStatus;
  size?: 'sm' | 'md';
}

export function StatusChip({ status, size = 'md' }: StatusChipProps) {
  const c = config[status];
  const padding = size === 'sm' ? '2px 8px' : '4px 10px';
  const fontSize = size === 'sm' ? '0.75rem' : '0.8125rem';

  return (
    <span
      style={{
        display: 'inline-flex',
        alignItems: 'center',
        gap: '6px',
        background: c.bg,
        color: c.text,
        borderRadius: '999px',
        padding,
        fontSize,
        fontWeight: 500,
        fontFamily: 'var(--font-body)',
        whiteSpace: 'nowrap',
      }}
    >
      <span
        style={{
          width: 6,
          height: 6,
          borderRadius: '50%',
          background: c.dot,
          flexShrink: 0,
        }}
      />
      {c.label}
    </span>
  );
}
