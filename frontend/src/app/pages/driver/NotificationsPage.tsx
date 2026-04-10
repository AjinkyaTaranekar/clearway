import { useEffect } from 'react';
import { useNavigate } from 'react-router';
import { useApp } from '../../context/AppContext';
import { CheckCircle2, AlertCircle, Info, AlertTriangle, CheckCheck, ArrowRight } from 'lucide-react';
import { formatDistanceToNow, parseISO } from 'date-fns';

export default function NotificationsPage() {
  const { notifications, markNotificationRead, markAllRead, unreadCount, refreshNotifications, user } = useApp();
  const navigate = useNavigate();

  useEffect(() => {
    void refreshNotifications();
  }, [refreshNotifications]);

  const getIcon = (type: string) => {
    if (type === 'success') return <CheckCircle2 size={16} color="#2E7D32" />;
    if (type === 'error') return <AlertCircle size={16} color="#B42318" />;
    if (type === 'warning') return <AlertTriangle size={16} color="#A15C00" />;
    return <Info size={16} color="#1A4E80" />;
  };

  const getBg = (type: string) => {
    if (type === 'success') return '#E8F4ED';
    if (type === 'error') return '#FDECEA';
    if (type === 'warning') return '#FFF4E0';
    return '#E3EEFB';
  };

  const handleClick = (n: typeof notifications[0]) => {
    markNotificationRead(n.id);
    if (n.journeyId) {
      const base = user?.role === 'admin' ? '/admin' : '/driver';
      navigate(`${base}/journeys/${n.journeyId}`);
    }
  };

  const formatTime = (ts: string) => {
    try {
      return formatDistanceToNow(parseISO(ts), { addSuffix: true });
    } catch {
      return ts;
    }
  };

  return (
    <div className="p-5 lg:p-8 max-w-2xl mx-auto">
      <div className="flex items-center justify-between mb-7">
        <div>
          <h1 style={{ fontFamily: 'var(--font-heading)', fontWeight: 700, color: '#1F2421', marginBottom: '4px' }}>
            Notifications
          </h1>
          <p style={{ color: '#4E5953', fontSize: '0.9375rem' }}>
            {unreadCount > 0 ? `${unreadCount} unread` : 'All caught up'}
          </p>
        </div>
        {unreadCount > 0 && (
          <button
            onClick={markAllRead}
            className="flex items-center gap-1.5 text-sm transition-opacity hover:opacity-70"
            style={{ color: '#2F6B55', fontWeight: 500 }}
          >
            <CheckCheck size={16} />
            Mark all as read
          </button>
        )}
      </div>

      {notifications.length === 0 ? (
        <div
          className="bg-white rounded-xl p-10 text-center"
          style={{ border: '1px solid var(--border)' }}
        >
          <div
            className="w-12 h-12 rounded-full flex items-center justify-center mx-auto mb-3"
            style={{ background: '#F0EDE7' }}
          >
            <CheckCheck size={20} color="#4E5953" />
          </div>
          <p style={{ color: '#1F2421', fontWeight: 600, fontFamily: 'var(--font-heading)', marginBottom: '4px' }}>
            No notifications yet
          </p>
          <p style={{ color: '#4E5953', fontSize: '0.875rem' }}>
            Updates about your journeys will appear here.
          </p>
        </div>
      ) : (
        <div className="bg-white rounded-xl overflow-hidden" style={{ border: '1px solid var(--border)' }}>
          {notifications.map((n, i) => (
            <div
              key={n.id}
              onClick={() => handleClick(n)}
              className="flex items-start gap-4 p-4 cursor-pointer hover:bg-muted transition-colors"
              style={{
                borderBottom: i < notifications.length - 1 ? '1px solid var(--border)' : 'none',
                background: n.read ? 'white' : '#FAFCF9',
              }}
              role="button"
              tabIndex={0}
              onKeyDown={(e) => e.key === 'Enter' && handleClick(n)}
            >
              <div
                className="w-9 h-9 rounded-full flex items-center justify-center flex-shrink-0 mt-0.5"
                style={{ background: getBg(n.type) }}
              >
                {getIcon(n.type)}
              </div>

              <div className="flex-1 min-w-0">
                <div className="flex items-center gap-2 mb-0.5">
                  <span
                    style={{
                      color: '#1F2421',
                      fontWeight: n.read ? 400 : 600,
                      fontSize: '0.9375rem',
                    }}
                  >
                    {n.title}
                  </span>
                  {!n.read && (
                    <span
                      className="w-2 h-2 rounded-full flex-shrink-0"
                      style={{ background: '#2F6B55' }}
                    />
                  )}
                </div>
                <p style={{ color: '#4E5953', fontSize: '0.875rem', lineHeight: 1.55 }}>
                  {n.message}
                </p>
                <div className="flex items-center gap-2 mt-1.5">
                  <span style={{ color: '#4E5953', fontSize: '0.8125rem' }}>
                    {formatTime(n.timestamp)}
                  </span>
                  {n.journeyId && (
                    <>
                      <span style={{ color: '#D9D2C7' }}>·</span>
                      <span
                        className="flex items-center gap-1 text-sm"
                        style={{ color: '#2F6B55', fontWeight: 500 }}
                      >
                        {n.journeyId} <ArrowRight size={12} />
                      </span>
                    </>
                  )}
                </div>
              </div>
            </div>
          ))}
        </div>
      )}

      {/* In-app note */}
      <div
        className="mt-5 p-4 rounded-xl flex items-start gap-3"
        style={{ background: '#F0EDE7' }}
      >
        <Info size={15} color="#4E5953" className="flex-shrink-0 mt-0.5" />
        <p style={{ color: '#4E5953', fontSize: '0.8125rem', lineHeight: 1.55 }}>
          This is your in-app notification centre. Every journey update appears here, even if push notifications are turned off on your device.
        </p>
      </div>
    </div>
  );
}
