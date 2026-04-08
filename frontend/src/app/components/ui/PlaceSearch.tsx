/**
 * PlaceSearch — debounced text input that calls the backend TomTom search proxy.
 * The API key never reaches the browser; all TomTom API calls happen server-side.
 */

import { MapPin, X } from 'lucide-react';
import { useEffect, useRef, useState } from 'react';
import { PlaceResult, searchPlaces } from '../../services/mapApi';

export interface PlaceSearchProps {
  id?: string;
  label?: string;
  placeholder?: string;
  value: PlaceResult | null;
  onChange: (place: PlaceResult | null) => void;
  pinColor?: string;
  error?: string;
  disabled?: boolean;
}

export default function PlaceSearch({
  id,
  placeholder = 'Search for a location',
  value,
  onChange,
  pinColor = '#4E5953',
  error,
  disabled = false,
}: PlaceSearchProps) {
  const [query, setQuery] = useState(value?.name ?? '');
  const [results, setResults] = useState<PlaceResult[]>([]);
  const [loading, setLoading] = useState(false);
  const [open, setOpen] = useState(false);
  const debounceRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const containerRef = useRef<HTMLDivElement>(null);

  // Sync display text when value changes externally
  useEffect(() => {
    setQuery(value?.name ?? '');
  }, [value]);

  const handleInput = (q: string) => {
    setQuery(q);
    if (!q.trim()) {
      onChange(null);
      setResults([]);
      setOpen(false);
      return;
    }

    if (debounceRef.current) clearTimeout(debounceRef.current);
    debounceRef.current = setTimeout(async () => {
      setLoading(true);
      try {
        const places = await searchPlaces(q, 6);
        setResults(places);
        setOpen(places.length > 0);
      } catch {
        setResults([]);
      } finally {
        setLoading(false);
      }
    }, 350);
  };

  const handleSelect = (place: PlaceResult) => {
    onChange(place);
    setQuery(place.name);
    setResults([]);
    setOpen(false);
  };

  const handleClear = () => {
    onChange(null);
    setQuery('');
    setResults([]);
    setOpen(false);
  };

  // Close dropdown on outside click
  useEffect(() => {
    const handler = (e: MouseEvent) => {
      if (containerRef.current && !containerRef.current.contains(e.target as Node)) {
        setOpen(false);
      }
    };
    document.addEventListener('mousedown', handler);
    return () => document.removeEventListener('mousedown', handler);
  }, []);

  return (
    <div ref={containerRef} className="relative">
      <div className="relative">
        <MapPin size={16} color={pinColor} className="absolute left-3 top-1/2 -translate-y-1/2 pointer-events-none" />
        <input
          id={id}
          type="text"
          value={query}
          onChange={(e) => handleInput(e.target.value)}
          onFocus={() => results.length > 0 && setOpen(true)}
          placeholder={placeholder}
          disabled={disabled}
          autoComplete="off"
          className="w-full pl-9 pr-9 py-2.5 rounded-lg outline-none"
          style={{
            border: error ? '1.5px solid #B42318' : '1.5px solid var(--border)',
            background: disabled ? '#F8F6F2' : 'white',
            color: '#1F2421',
          }}
        />
        {(query || loading) && (
          <button
            type="button"
            onClick={handleClear}
            className="absolute right-3 top-1/2 -translate-y-1/2"
            aria-label="Clear"
          >
            {loading ? (
              <span className="w-3.5 h-3.5 border-2 border-gray-300 border-t-gray-600 rounded-full animate-spin block" />
            ) : (
              <X size={14} color="#4E5953" />
            )}
          </button>
        )}
      </div>

      {open && results.length > 0 && (
        <ul
          className="absolute z-50 w-full mt-1 rounded-xl overflow-hidden shadow-lg"
          style={{ border: '1px solid var(--border)', background: 'white' }}
        >
          {results.map((place) => (
            <li key={place.place_id}>
              <button
                type="button"
                className="w-full flex items-start gap-3 px-4 py-3 text-left hover:bg-muted transition-colors"
                onClick={() => handleSelect(place)}
              >
                <MapPin size={14} color="#4E5953" className="flex-shrink-0 mt-0.5" />
                <div className="min-w-0">
                  <div style={{ fontWeight: 500, color: '#1F2421', fontSize: '0.875rem' }} className="truncate">
                    {place.name}
                  </div>
                  {place.address && place.address !== place.name && (
                    <div style={{ color: '#4E5953', fontSize: '0.75rem' }} className="truncate">
                      {place.address}
                    </div>
                  )}
                </div>
              </button>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}
