import React, { useState } from 'react';
import { useNavigate } from 'react-router';
import { AlertCircle, Eye, EyeOff } from 'lucide-react';
import { useApp } from '../context/AppContext';

type Mode = 'login' | 'register';
const VEHICLE_TYPES = ['car', 'van', 'truck', 'motorcycle'] as const;
const DEMO_CREDENTIALS = {
  driver: {
    label: 'Driver demo',
    email: 'ajinkyataranekar26@gmail.com',
    password: 'test1234',
  },
  admin: {
    label: 'Admin demo',
    email: 'admin@local.vcs',
    password: 'admin123',
  },
} as const;

export default function LoginPage() {
  const navigate = useNavigate();
  const { login, register, isAuthenticated, user } = useApp();

  const [mode, setMode] = useState<Mode>('login');

  const [email, setEmail] = useState('');
  const [password, setPassword] = useState('');
  const [showPassword, setShowPassword] = useState(false);

  const [name, setName] = useState('');
  const [vehicleType, setVehicleType] = useState<string>('car');
  const [licenseNumber, setLicenseNumber] = useState('');

  const [errors, setErrors] = useState<Record<string, string>>({});
  const [loading, setLoading] = useState(false);

  if (isAuthenticated && user) {
    navigate(user.role === 'admin' ? '/admin' : '/driver');
  }

  const validateLogin = () => {
    const e: Record<string, string> = {};
    if (!email) e.email = 'Email is required.';
    else if (!/\S+@\S+\.\S+/.test(email)) e.email = 'Enter a valid email address.';
    if (!password) e.password = 'Password is required.';
    else if (password.length < 8) e.password = 'Password must be at least 8 characters.';
    return e;
  };

  const validateRegister = () => {
    const e: Record<string, string> = {};
    if (!name.trim()) e.name = 'Full name is required.';
    if (!email) e.email = 'Email is required.';
    else if (!/\S+@\S+\.\S+/.test(email)) e.email = 'Enter a valid email address.';
    if (!password) e.password = 'Password is required.';
    else if (password.length < 8) e.password = 'Min 8 characters, at least 1 letter and 1 digit.';
    else if (!/[a-zA-Z]/.test(password) || !/\d/.test(password))
      e.password = 'Min 8 characters, at least 1 letter and 1 digit.';
    if (!licenseNumber.trim()) e.licenseNumber = 'License number is required.';
    return e;
  };

  const onSwitch = (m: Mode) => {
    setMode(m);
    setErrors({});
  };

  const applyDemoCredentials = (demo: (typeof DEMO_CREDENTIALS)[keyof typeof DEMO_CREDENTIALS]) => {
    setMode('login');
    setEmail(demo.email);
    setPassword(demo.password);
    setErrors({});
  };

  const onFocus = (e: React.FocusEvent<HTMLInputElement | HTMLSelectElement>) =>
    (e.target.style.borderColor = '#2F6B55');

  const onBlurField =
    (field: string) => (e: React.FocusEvent<HTMLInputElement | HTMLSelectElement>) =>
      (e.target.style.borderColor = errors[field] ? '#B42318' : 'var(--border)');

  const inputStyle = (field: string): React.CSSProperties => ({
    border: errors[field] ? '1.5px solid #B42318' : '1.5px solid var(--border)',
    background: 'white',
    color: '#1F2421',
  });

  const handleLogin = async (e: React.FormEvent) => {
    e.preventDefault();
    const errs = validateLogin();
    if (Object.keys(errs).length) return setErrors(errs);
    setErrors({});
    setLoading(true);
    try {
      const role = await login(email, password);
      navigate(role === 'admin' ? '/admin' : '/driver');
    } catch (err: any) {
      setErrors({ form: err.message ?? 'Login failed. Please check your credentials.' });
    } finally {
      setLoading(false);
    }
  };

  const handleRegister = async (e: React.FormEvent) => {
    e.preventDefault();
    const errs = validateRegister();
    if (Object.keys(errs).length) return setErrors(errs);
    setErrors({});
    setLoading(true);
    try {
      const role = await register({ name, email, password, vehicleType, licenseNumber });
      navigate(role === 'admin' ? '/admin' : '/driver');
    } catch (err: any) {
      setErrors({ form: err.message ?? 'Registration failed. Please try again.' });
    } finally {
      setLoading(false);
    }
  };

  return (
    <div className="min-h-screen flex items-center justify-center p-6" style={{ background: '#F8F6F2' }}>
      <div className="w-full max-w-md">
        <div className="mb-6 text-center">
          <div style={{ fontFamily: 'var(--font-heading)', fontWeight: 800, fontSize: '1.75rem', color: '#1F2421' }}>
            Clearway
          </div>
          <div style={{ color: '#4E5953', fontSize: '0.95rem' }}>
            {mode === 'login' ? 'Sign in to continue.' : 'Create a driver account to book journeys.'}
          </div>
        </div>

        <div className="bg-white rounded-2xl p-8 shadow-sm" style={{ border: '1px solid var(--border)' }}>
          <div className="flex rounded-lg p-1 mb-6" style={{ background: '#F0EDE7' }}>
            {(['login', 'register'] as Mode[]).map((m) => (
              <button
                key={m}
                type="button"
                onClick={() => onSwitch(m)}
                className="flex-1 py-2 rounded-md text-sm transition-all duration-150"
                style={{
                  background: mode === m ? 'white' : 'transparent',
                  color: mode === m ? '#1F2421' : '#4E5953',
                  fontWeight: mode === m ? 600 : 400,
                  boxShadow: mode === m ? '0 1px 3px rgba(0,0,0,0.08)' : 'none',
                  fontFamily: 'var(--font-body)',
                }}
              >
                {m === 'login' ? 'Sign in' : 'Create account'}
              </button>
            ))}
          </div>

          {errors.form && (
            <div
              className="flex items-center gap-2 p-3 rounded-lg mb-4"
              style={{ background: 'var(--status-rejected-bg)', color: 'var(--status-rejected-text)' }}
              role="alert"
            >
              <AlertCircle size={16} />
              <span className="text-sm">{errors.form}</span>
            </div>
          )}

          {mode === 'login' ? (
            <form onSubmit={handleLogin} noValidate>
              <div
                className="mb-4 p-3 rounded-lg"
                style={{ background: '#F0EDE7', border: '1px dashed var(--border)' }}
              >
                <div style={{ color: '#1F2421', fontSize: '0.8125rem', fontWeight: 600, marginBottom: '0.625rem' }}>
                  Quick demo access
                </div>
                <div className="grid grid-cols-1 sm:grid-cols-2 gap-2">
                  {Object.values(DEMO_CREDENTIALS).map((demo) => (
                    <button
                      key={demo.label}
                      type="button"
                      onClick={() => applyDemoCredentials(demo)}
                      className="w-full py-2 rounded-lg text-sm transition-all duration-150"
                      style={{
                        border: '1px solid var(--border)',
                        background: 'white',
                        color: '#1F2421',
                        fontWeight: 500,
                      }}
                    >
                      {demo.label}
                    </button>
                  ))}
                </div>
              </div>

              <div className="space-y-4">
                <div>
                  <label className="block mb-1.5" style={{ color: '#1F2421', fontSize: '0.875rem', fontWeight: 500 }}>
                    Email address
                  </label>
                  <input
                    type="email"
                    value={email}
                    onChange={(e) => setEmail(e.target.value)}
                    placeholder="you@example.com"
                    aria-invalid={!!errors.email}
                    className="w-full px-3.5 py-2.5 rounded-lg outline-none transition-all"
                    style={inputStyle('email')}
                    onFocus={onFocus}
                    onBlur={onBlurField('email')}
                  />
                  {errors.email && <p className="mt-1.5 text-sm" style={{ color: '#B42318' }}>{errors.email}</p>}
                </div>

                <div>
                  <label className="block mb-1.5" style={{ color: '#1F2421', fontSize: '0.875rem', fontWeight: 500 }}>
                    Password
                  </label>
                  <div className="relative">
                    <input
                      type={showPassword ? 'text' : 'password'}
                      value={password}
                      onChange={(e) => setPassword(e.target.value)}
                      placeholder="••••••••"
                      aria-invalid={!!errors.password}
                      className="w-full px-3.5 py-2.5 pr-10 rounded-lg outline-none transition-all"
                      style={inputStyle('password')}
                      onFocus={onFocus}
                      onBlur={onBlurField('password')}
                    />
                    <button
                      type="button"
                      onClick={() => setShowPassword((v) => !v)}
                      className="absolute right-3 top-1/2 -translate-y-1/2"
                      aria-label={showPassword ? 'Hide password' : 'Show password'}
                    >
                      {showPassword ? <EyeOff size={16} color="#4E5953" /> : <Eye size={16} color="#4E5953" />}
                    </button>
                  </div>
                  {errors.password && <p className="mt-1.5 text-sm" style={{ color: '#B42318' }}>{errors.password}</p>}
                </div>
              </div>

              <button
                type="submit"
                disabled={loading}
                className="w-full mt-6 py-3 rounded-lg transition-all duration-150 flex items-center justify-center gap-2"
                style={{
                  background: loading ? '#4E5953' : '#2F6B55',
                  color: 'white',
                  fontWeight: 600,
                  fontSize: '0.9375rem',
                  cursor: loading ? 'not-allowed' : 'pointer',
                }}
              >
                {loading ? (
                  <>
                    <span className="w-4 h-4 border-2 border-white/30 border-t-white rounded-full animate-spin" />
                    Signing in…
                  </>
                ) : (
                  'Sign in'
                )}
              </button>
            </form>
          ) : (
            <form onSubmit={handleRegister} noValidate>
              <div className="space-y-4">
                <div>
                  <label className="block mb-1.5" style={{ color: '#1F2421', fontSize: '0.875rem', fontWeight: 500 }}>
                    Full name
                  </label>
                  <input
                    type="text"
                    value={name}
                    onChange={(e) => setName(e.target.value)}
                    placeholder="Alex Chen"
                    className="w-full px-3.5 py-2.5 rounded-lg outline-none transition-all"
                    style={inputStyle('name')}
                    onFocus={onFocus}
                    onBlur={onBlurField('name')}
                  />
                  {errors.name && <p className="mt-1.5 text-sm" style={{ color: '#B42318' }}>{errors.name}</p>}
                </div>

                <div>
                  <label className="block mb-1.5" style={{ color: '#1F2421', fontSize: '0.875rem', fontWeight: 500 }}>
                    Email address
                  </label>
                  <input
                    type="email"
                    value={email}
                    onChange={(e) => setEmail(e.target.value)}
                    placeholder="you@example.com"
                    className="w-full px-3.5 py-2.5 rounded-lg outline-none transition-all"
                    style={inputStyle('email')}
                    onFocus={onFocus}
                    onBlur={onBlurField('email')}
                  />
                  {errors.email && <p className="mt-1.5 text-sm" style={{ color: '#B42318' }}>{errors.email}</p>}
                </div>

                <div>
                  <label className="block mb-1.5" style={{ color: '#1F2421', fontSize: '0.875rem', fontWeight: 500 }}>
                    Password
                  </label>
                  <div className="relative">
                    <input
                      type={showPassword ? 'text' : 'password'}
                      value={password}
                      onChange={(e) => setPassword(e.target.value)}
                      placeholder="Min 8 chars, 1 letter + 1 digit"
                      className="w-full px-3.5 py-2.5 pr-10 rounded-lg outline-none transition-all"
                      style={inputStyle('password')}
                      onFocus={onFocus}
                      onBlur={onBlurField('password')}
                    />
                    <button
                      type="button"
                      onClick={() => setShowPassword((v) => !v)}
                      className="absolute right-3 top-1/2 -translate-y-1/2"
                      aria-label={showPassword ? 'Hide password' : 'Show password'}
                    >
                      {showPassword ? <EyeOff size={16} color="#4E5953" /> : <Eye size={16} color="#4E5953" />}
                    </button>
                  </div>
                  {errors.password && <p className="mt-1.5 text-sm" style={{ color: '#B42318' }}>{errors.password}</p>}
                </div>

                <div>
                  <label className="block mb-1.5" style={{ color: '#1F2421', fontSize: '0.875rem', fontWeight: 500 }}>
                    Vehicle type
                  </label>
                  <select
                    value={vehicleType}
                    onChange={(e) => setVehicleType(e.target.value)}
                    className="w-full px-3.5 py-2.5 rounded-lg outline-none appearance-none"
                    style={inputStyle('vehicleType')}
                    onFocus={onFocus}
                    onBlur={onBlurField('vehicleType')}
                  >
                    {VEHICLE_TYPES.map((v) => (
                      <option key={v} value={v}>
                        {v}
                      </option>
                    ))}
                  </select>
                </div>

                <div>
                  <label className="block mb-1.5" style={{ color: '#1F2421', fontSize: '0.875rem', fontWeight: 500 }}>
                    License number
                  </label>
                  <input
                    type="text"
                    value={licenseNumber}
                    onChange={(e) => setLicenseNumber(e.target.value)}
                    placeholder="DL-123456"
                    className="w-full px-3.5 py-2.5 rounded-lg outline-none transition-all"
                    style={inputStyle('licenseNumber')}
                    onFocus={onFocus}
                    onBlur={onBlurField('licenseNumber')}
                  />
                  {errors.licenseNumber && (
                    <p className="mt-1.5 text-sm" style={{ color: '#B42318' }}>{errors.licenseNumber}</p>
                  )}
                </div>
              </div>

              <button
                type="submit"
                disabled={loading}
                className="w-full mt-6 py-3 rounded-lg transition-all duration-150 flex items-center justify-center gap-2"
                style={{
                  background: loading ? '#4E5953' : '#2F6B55',
                  color: 'white',
                  fontWeight: 600,
                  fontSize: '0.9375rem',
                  cursor: loading ? 'not-allowed' : 'pointer',
                }}
              >
                {loading ? (
                  <>
                    <span className="w-4 h-4 border-2 border-white/30 border-t-white rounded-full animate-spin" />
                    Creating account…
                  </>
                ) : (
                  'Create account'
                )}
              </button>
            </form>
          )}
        </div>
      </div>
    </div>
  );
}
