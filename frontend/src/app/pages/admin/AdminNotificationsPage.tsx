import { AlertCircle, CheckCircle2, Info, Loader2, Send, ShieldAlert } from 'lucide-react';
import { useEffect, useState } from 'react';
import { formatDistanceToNow, parseISO } from 'date-fns';
import { listAdminNotifications, type AdminApiNotification } from '../../services/notificationApi';

const statusColors: Record<string, { bg: string; fg: string }> = {
  SENT: { bg: '#E8F4ED', fg: '#2E7D32' },
  FAILED: { bg: '#FDECEA', fg: '#B42318' },
  SKIPPED: { bg: '#FFF4E0', fg: '#A15C00' },
  RETRYING: { bg: '#E3EEFB', fg: '#1A4E80' },
  PENDING: { bg: '#F0EDE7', fg: '#4E5953' },
};

const typeIcon = (type: string) => {
  if (type === 'success') return <CheckCircle2 size={16} color="#2E7D32" />;
  if (type === 'error') return <AlertCircle size={16} color="#B42318" />;
  return <Info size={16} color="#1A4E80" />;
};

function formatTime(ts: string) {
  try {
    return formatDistanceToNow(parseISO(ts), { addSuffix: true });
  } catch {
    return ts;
  }
}

export default function AdminNotificationsPage() {
  const [notifications, setNotifications] = useState<AdminApiNotification[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');

  useEffect(() => {
    const load = async () => {
      setLoading(true);
      setError('');
      try {
        const res = await listAdminNotifications();
        setNotifications(res.notifications);
      } catch (err: any) {
        setError(err.message ?? 'Failed to load admin notifications.');
      } finally {
        setLoading(false);
      }
    };

    load();
  }, []);

  return (
    <div className="p-5 lg:p-8 max-w-4xl mx-auto">
      <div className="flex items-center gap-3 mb-7">
        <div className="w-11 h-11 rounded-full flex items-center justify-center" style={{ background: '#F0EDE7' }}>
          <ShieldAlert size={20} color="#2F6B55" />
        </div>
        <div>
          <h1 style={{ fontFamily: 'var(--font-heading)', fontWeight: 700, color: '#1F2421', marginBottom: '4px' }}>
            System notifications
          </h1>
          <p style={{ color: '#4E5953', fontSize: '0.9375rem' }}>
            Delivery outcomes for all driver journey notifications.
          </p>
        </div>
      </div>

      {loading ? (
        <div className="bg-white rounded-xl p-10 flex items-center justify-center gap-3" style={{ border: '1px solid var(--border)' }}>
          <Loader2 size={18} className="animate-spin" color="#2F6B55" />
          <span style={{ color: '#4E5953' }}>Loading notification activity…</span>
        </div>
      ) : error ? (
        <div className="bg-white rounded-xl p-5 flex items-center gap-3" style={{ border: '1px solid var(--border)', color: '#B42318' }}>
          <AlertCircle size={18} />
          <span>{error}</span>
        </div>
      ) : notifications.length === 0 ? (
        <div className="bg-white rounded-xl p-10 text-center" style={{ border: '1px solid var(--border)' }}>
          <Send size={20} color="#4E5953" className="mx-auto mb-3" />
          <p style={{ color: '#1F2421', fontWeight: 600, fontFamily: 'var(--font-heading)', marginBottom: '4px' }}>
            No system notifications yet
          </p>
          <p style={{ color: '#4E5953', fontSize: '0.875rem' }}>
            Journey notification events will appear here once they are created.
          </p>
        </div>
      ) : (
        <div className="bg-white rounded-xl overflow-hidden" style={{ border: '1px solid var(--border)' }}>
          {notifications.map((notification, index) => {
            const delivery = statusColors[notification.delivery_status] ?? statusColors.PENDING;
            return (
              <div
                key={notification.id}
                className="p-4"
                style={{ borderBottom: index < notifications.length - 1 ? '1px solid var(--border)' : 'none' }}
              >
                <div className="flex items-start gap-4">
                  <div className="w-9 h-9 rounded-full flex items-center justify-center flex-shrink-0 mt-0.5" style={{ background: '#F8F6F2' }}>
                    {typeIcon(notification.type)}
                  </div>

                  <div className="flex-1 min-w-0">
                    <div className="flex flex-wrap items-center gap-2 mb-1.5">
                      <span style={{ color: '#1F2421', fontWeight: 600, fontSize: '0.9375rem' }}>
                        {notification.title}
                      </span>
                      <span
                        className="px-2 py-1 rounded-full text-xs"
                        style={{ background: delivery.bg, color: delivery.fg, fontWeight: 600 }}
                      >
                        {notification.delivery_status}
                      </span>
                    </div>

                    <p style={{ color: '#4E5953', fontSize: '0.875rem', lineHeight: 1.55 }}>
                      {notification.message}
                    </p>

                    <div className="flex flex-wrap items-center gap-2 mt-2" style={{ color: '#4E5953', fontSize: '0.8125rem' }}>
                      <span>Driver: {notification.driver_id}</span>
                      <span>·</span>
                      <span>Journey: {notification.journey_id}</span>
                      <span>·</span>
                      <span>{formatTime(notification.timestamp)}</span>
                    </div>
                  </div>
                </div>
              </div>
            );
          })}
        </div>
      )}
    </div>
  );
}
