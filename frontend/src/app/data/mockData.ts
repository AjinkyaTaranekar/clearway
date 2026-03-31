export type JourneyStatus = 'pending' | 'approved' | 'rejected' | 'active' | 'completed' | 'cancelled';
export type VehicleType = 'Car' | 'Van' | 'Motorcycle' | 'HGV';
export type TrafficLevel = 'low' | 'medium' | 'high' | 'critical';
export type Region = 'North' | 'South' | 'East' | 'West' | 'Central';

export interface RouteSegment {
  id: string;
  name: string;
  occupancy: number;
  level: TrafficLevel;
}

export interface TimelineEvent {
  id: string;
  type: string;
  label: string;
  timestamp: string;
  by?: string;
}

export interface Journey {
  id: string;
  driverId: string;
  driverName: string;
  origin: string;
  destination: string;
  departureTime: string;
  estimatedArrival: string;
  vehicleType: VehicleType;
  status: JourneyStatus;
  region: Region;
  rejectionReason?: string;
  segments: RouteSegment[];
  timeline: TimelineEvent[];
  createdAt: string;
  updatedAt: string;
  distance: string;
  duration: string;
}

export interface Notification {
  id: string;
  title: string;
  message: string;
  type: 'info' | 'success' | 'warning' | 'error';
  read: boolean;
  timestamp: string;
  journeyId?: string;
}

export interface TrafficSegment {
  id: string;
  name: string;
  region: Region;
  level: TrafficLevel;
  occupancy: number;
  trend: 'improving' | 'stable' | 'worsening';
  vehicles: number;
  capacity: number;
  fromNode: string;
  toNode: string;
}

export interface MapNode {
  id: string;
  label: string;
  x: number;
  y: number;
}

export const mapNodes: MapNode[] = [
  { id: 'city', label: 'City Centre', x: 300, y: 250 },
  { id: 'north', label: 'North Gate', x: 300, y: 60 },
  { id: 'airport', label: 'Airport', x: 500, y: 60 },
  { id: 'east', label: 'East Quay', x: 520, y: 250 },
  { id: 'south', label: 'South Terminal', x: 300, y: 420 },
  { id: 'industrial', label: 'Industrial Park', x: 470, y: 400 },
  { id: 'west', label: 'West Depot', x: 80, y: 250 },
  { id: 'port', label: 'Port Terminal', x: 80, y: 390 },
  { id: 'northfield', label: 'Northfield', x: 130, y: 80 },
  { id: 'riverside', label: 'Riverside', x: 200, y: 380 },
];

export const trafficSegments: TrafficSegment[] = [
  { id: 'TS01', name: 'North Ring Road', region: 'North', level: 'high', occupancy: 78, trend: 'worsening', vehicles: 234, capacity: 300, fromNode: 'north', toNode: 'city' },
  { id: 'TS02', name: 'Airport Approach', region: 'North', level: 'low', occupancy: 31, trend: 'stable', vehicles: 93, capacity: 300, fromNode: 'north', toNode: 'airport' },
  { id: 'TS03', name: 'East Corridor', region: 'East', level: 'medium', occupancy: 55, trend: 'stable', vehicles: 165, capacity: 300, fromNode: 'city', toNode: 'east' },
  { id: 'TS04', name: 'East Ring Road', region: 'East', level: 'critical', occupancy: 95, trend: 'worsening', vehicles: 285, capacity: 300, fromNode: 'airport', toNode: 'east' },
  { id: 'TS05', name: 'West Ring Road', region: 'West', level: 'high', occupancy: 82, trend: 'worsening', vehicles: 246, capacity: 300, fromNode: 'west', toNode: 'city' },
  { id: 'TS06', name: 'Port Road', region: 'West', level: 'critical', occupancy: 91, trend: 'worsening', vehicles: 273, capacity: 300, fromNode: 'west', toNode: 'port' },
  { id: 'TS07', name: 'South Bridge', region: 'South', level: 'low', occupancy: 38, trend: 'stable', vehicles: 114, capacity: 300, fromNode: 'city', toNode: 'south' },
  { id: 'TS08', name: 'Industrial Road', region: 'South', level: 'medium', occupancy: 62, trend: 'stable', vehicles: 186, capacity: 300, fromNode: 'south', toNode: 'industrial' },
  { id: 'TS09', name: 'Riverside Drive', region: 'Central', level: 'low', occupancy: 29, trend: 'improving', vehicles: 87, capacity: 300, fromNode: 'riverside', toNode: 'south' },
  { id: 'TS10', name: 'High Street', region: 'Central', level: 'medium', occupancy: 64, trend: 'stable', vehicles: 192, capacity: 300, fromNode: 'city', toNode: 'riverside' },
  { id: 'TS11', name: 'Northfield Link', region: 'North', level: 'low', occupancy: 33, trend: 'improving', vehicles: 99, capacity: 300, fromNode: 'northfield', toNode: 'north' },
  { id: 'TS12', name: 'Airport Industrial', region: 'East', level: 'medium', occupancy: 58, trend: 'stable', vehicles: 174, capacity: 300, fromNode: 'east', toNode: 'industrial' },
];

export const mockJourneys: Journey[] = [
  {
    id: 'J-2847',
    driverId: 'D001',
    driverName: 'Alex Chen',
    origin: 'City Centre',
    destination: 'Airport North',
    departureTime: '2026-03-30T14:00',
    estimatedArrival: '2026-03-30T14:45',
    vehicleType: 'Car',
    status: 'approved',
    region: 'North',
    segments: [
      { id: 'S1', name: 'High Street', occupancy: 42, level: 'low' },
      { id: 'S2', name: 'Ring Road North', occupancy: 67, level: 'medium' },
      { id: 'S3', name: 'Airport Approach', occupancy: 31, level: 'low' },
    ],
    timeline: [
      { id: 'T1', type: 'created', label: 'Journey booked', timestamp: '2026-03-30T08:00', by: 'You' },
      { id: 'T2', type: 'approved', label: 'Journey approved', timestamp: '2026-03-30T08:01', by: 'System' },
    ],
    createdAt: '2026-03-30T08:00',
    updatedAt: '2026-03-30T08:01',
    distance: '18.3 km',
    duration: '45 min',
  },
  {
    id: 'J-2831',
    driverId: 'D001',
    driverName: 'Alex Chen',
    origin: 'Westside Depot',
    destination: 'Central Market',
    departureTime: '2026-03-30T11:30',
    estimatedArrival: '2026-03-30T12:00',
    vehicleType: 'Van',
    status: 'active',
    region: 'West',
    segments: [
      { id: 'S1', name: 'Westside Road', occupancy: 55, level: 'medium' },
      { id: 'S2', name: 'Market Street', occupancy: 78, level: 'high' },
    ],
    timeline: [
      { id: 'T1', type: 'created', label: 'Journey booked', timestamp: '2026-03-29T15:00', by: 'You' },
      { id: 'T2', type: 'approved', label: 'Journey approved', timestamp: '2026-03-29T15:01', by: 'System' },
      { id: 'T3', type: 'active', label: 'Journey started', timestamp: '2026-03-30T11:31', by: 'You' },
    ],
    createdAt: '2026-03-29T15:00',
    updatedAt: '2026-03-30T11:31',
    distance: '12.1 km',
    duration: '30 min',
  },
  {
    id: 'J-2819',
    driverId: 'D001',
    driverName: 'Alex Chen',
    origin: 'South Terminal',
    destination: 'Industrial Park',
    departureTime: '2026-03-29T09:00',
    estimatedArrival: '2026-03-29T09:50',
    vehicleType: 'HGV',
    status: 'completed',
    region: 'South',
    segments: [
      { id: 'S1', name: 'South Bridge', occupancy: 30, level: 'low' },
      { id: 'S2', name: 'Industrial Road', occupancy: 45, level: 'low' },
    ],
    timeline: [
      { id: 'T1', type: 'created', label: 'Journey booked', timestamp: '2026-03-28T14:00', by: 'You' },
      { id: 'T2', type: 'approved', label: 'Journey approved', timestamp: '2026-03-28T14:02', by: 'System' },
      { id: 'T3', type: 'active', label: 'Journey started', timestamp: '2026-03-29T09:01', by: 'You' },
      { id: 'T4', type: 'completed', label: 'Journey completed', timestamp: '2026-03-29T09:52', by: 'You' },
    ],
    createdAt: '2026-03-28T14:00',
    updatedAt: '2026-03-29T09:52',
    distance: '24.5 km',
    duration: '50 min',
  },
  {
    id: 'J-2808',
    driverId: 'D001',
    driverName: 'Alex Chen',
    origin: 'East Quay',
    destination: 'University District',
    departureTime: '2026-03-30T08:00',
    estimatedArrival: '2026-03-30T08:35',
    vehicleType: 'Motorcycle',
    status: 'rejected',
    region: 'East',
    rejectionReason: 'East Ring Road is at full capacity at 08:00. One or more segments on your route cannot accommodate additional vehicles. Try selecting a departure after 09:30.',
    segments: [],
    timeline: [
      { id: 'T1', type: 'created', label: 'Journey booked', timestamp: '2026-03-30T07:00', by: 'You' },
      { id: 'T2', type: 'rejected', label: 'Journey rejected', timestamp: '2026-03-30T07:01', by: 'System' },
    ],
    createdAt: '2026-03-30T07:00',
    updatedAt: '2026-03-30T07:01',
    distance: '11.2 km',
    duration: '35 min',
  },
  {
    id: 'J-2795',
    driverId: 'D001',
    driverName: 'Alex Chen',
    origin: 'North Gate',
    destination: 'Riverside',
    departureTime: '2026-03-28T16:00',
    estimatedArrival: '2026-03-28T16:30',
    vehicleType: 'Car',
    status: 'cancelled',
    region: 'North',
    segments: [],
    timeline: [
      { id: 'T1', type: 'created', label: 'Journey booked', timestamp: '2026-03-28T10:00', by: 'You' },
      { id: 'T2', type: 'approved', label: 'Journey approved', timestamp: '2026-03-28T10:01', by: 'System' },
      { id: 'T3', type: 'cancelled', label: 'Journey cancelled', timestamp: '2026-03-28T12:00', by: 'You' },
    ],
    createdAt: '2026-03-28T10:00',
    updatedAt: '2026-03-28T12:00',
    distance: '9.8 km',
    duration: '30 min',
  },
];

export const allJourneys: Journey[] = [
  ...mockJourneys,
  {
    id: 'J-2855',
    driverId: 'D002',
    driverName: 'Maria Santos',
    origin: 'Northfield',
    destination: 'City Centre',
    departureTime: '2026-03-30T15:00',
    estimatedArrival: '2026-03-30T15:40',
    vehicleType: 'Car',
    status: 'pending',
    region: 'North',
    segments: [],
    timeline: [{ id: 'T1', type: 'created', label: 'Journey booked', timestamp: '2026-03-30T10:00', by: 'Maria Santos' }],
    createdAt: '2026-03-30T10:00',
    updatedAt: '2026-03-30T10:00',
    distance: '16.0 km',
    duration: '40 min',
  },
  {
    id: 'J-2849',
    driverId: 'D003',
    driverName: "James O'Brien",
    origin: 'West Industrial',
    destination: 'Port Terminal',
    departureTime: '2026-03-30T13:00',
    estimatedArrival: '2026-03-30T14:00',
    vehicleType: 'HGV',
    status: 'active',
    region: 'West',
    segments: [
      { id: 'S1', name: 'West Ring', occupancy: 82, level: 'high' },
      { id: 'S2', name: 'Port Road', occupancy: 91, level: 'critical' },
    ],
    timeline: [
      { id: 'T1', type: 'created', label: 'Journey booked', timestamp: '2026-03-30T09:00', by: "James O'Brien" },
      { id: 'T2', type: 'approved', label: 'Journey approved', timestamp: '2026-03-30T09:01', by: 'System' },
      { id: 'T3', type: 'active', label: 'Journey started', timestamp: '2026-03-30T13:01', by: "James O'Brien" },
    ],
    createdAt: '2026-03-30T09:00',
    updatedAt: '2026-03-30T13:01',
    distance: '22.7 km',
    duration: '60 min',
  },
  {
    id: 'J-2845',
    driverId: 'D004',
    driverName: 'Priya Patel',
    origin: 'Eastfield',
    destination: 'South Terminal',
    departureTime: '2026-03-30T10:00',
    estimatedArrival: '2026-03-30T10:45',
    vehicleType: 'Van',
    status: 'approved',
    region: 'East',
    segments: [
      { id: 'S1', name: 'East Corridor', occupancy: 48, level: 'low' },
      { id: 'S2', name: 'South Bridge', occupancy: 61, level: 'medium' },
    ],
    timeline: [
      { id: 'T1', type: 'created', label: 'Journey booked', timestamp: '2026-03-30T07:00', by: 'Priya Patel' },
      { id: 'T2', type: 'approved', label: 'Journey approved', timestamp: '2026-03-30T07:01', by: 'System' },
    ],
    createdAt: '2026-03-30T07:00',
    updatedAt: '2026-03-30T07:01',
    distance: '19.3 km',
    duration: '45 min',
  },
  {
    id: 'J-2840',
    driverId: 'D005',
    driverName: 'Tom Walsh',
    origin: 'City Centre',
    destination: 'North Suburb',
    departureTime: '2026-03-30T12:00',
    estimatedArrival: '2026-03-30T12:30',
    vehicleType: 'Car',
    status: 'rejected',
    region: 'North',
    rejectionReason: 'North Ring Road exceeds capacity at this departure time.',
    segments: [],
    timeline: [
      { id: 'T1', type: 'created', label: 'Journey booked', timestamp: '2026-03-30T08:00', by: 'Tom Walsh' },
      { id: 'T2', type: 'rejected', label: 'Journey rejected', timestamp: '2026-03-30T08:01', by: 'System' },
    ],
    createdAt: '2026-03-30T08:00',
    updatedAt: '2026-03-30T08:01',
    distance: '14.2 km',
    duration: '30 min',
  },
  {
    id: 'J-2835',
    driverId: 'D006',
    driverName: 'Lena Fischer',
    origin: 'Riverside',
    destination: 'West Depot',
    departureTime: '2026-03-30T09:00',
    estimatedArrival: '2026-03-30T09:35',
    vehicleType: 'Car',
    status: 'completed',
    region: 'West',
    segments: [],
    timeline: [
      { id: 'T1', type: 'created', label: 'Journey booked', timestamp: '2026-03-29T18:00', by: 'Lena Fischer' },
      { id: 'T2', type: 'approved', label: 'Journey approved', timestamp: '2026-03-29T18:01', by: 'System' },
      { id: 'T3', type: 'active', label: 'Journey started', timestamp: '2026-03-30T09:01', by: 'Lena Fischer' },
      { id: 'T4', type: 'completed', label: 'Journey completed', timestamp: '2026-03-30T09:36', by: 'Lena Fischer' },
    ],
    createdAt: '2026-03-29T18:00',
    updatedAt: '2026-03-30T09:36',
    distance: '13.6 km',
    duration: '35 min',
  },
];

export const mockNotifications: Notification[] = [
  {
    id: 'N001',
    title: 'Journey approved',
    message: 'Your journey from City Centre to Airport North has been approved. You can activate it at your departure time.',
    type: 'success',
    read: false,
    timestamp: '2026-03-30T08:01',
    journeyId: 'J-2847',
  },
  {
    id: 'N002',
    title: 'Journey rejected',
    message: 'Your journey from East Quay to University District could not be booked. East Ring Road is at full capacity at 08:00.',
    type: 'error',
    read: false,
    timestamp: '2026-03-30T07:01',
    journeyId: 'J-2808',
  },
  {
    id: 'N003',
    title: 'Reminder: Journey at 14:00',
    message: 'You have an approved journey from City Centre to Airport North in a few hours. Make sure your vehicle is ready.',
    type: 'info',
    read: false,
    timestamp: '2026-03-30T07:00',
    journeyId: 'J-2847',
  },
  {
    id: 'N004',
    title: 'Journey started',
    message: 'You have activated your journey from Westside Depot to Central Market. Drive safely.',
    type: 'info',
    read: true,
    timestamp: '2026-03-30T11:31',
    journeyId: 'J-2831',
  },
  {
    id: 'N005',
    title: 'Journey completed',
    message: 'Your journey from South Terminal to Industrial Park has been completed successfully.',
    type: 'success',
    read: true,
    timestamp: '2026-03-29T09:52',
    journeyId: 'J-2819',
  },
];

export const analyticsData = {
  bookingTrend: [
    { day: 'Mon', bookings: 45, approved: 38, rejected: 7 },
    { day: 'Tue', bookings: 52, approved: 44, rejected: 8 },
    { day: 'Wed', bookings: 61, approved: 50, rejected: 11 },
    { day: 'Thu', bookings: 48, approved: 41, rejected: 7 },
    { day: 'Fri', bookings: 73, approved: 59, rejected: 14 },
    { day: 'Sat', bookings: 38, approved: 35, rejected: 3 },
    { day: 'Sun', bookings: 29, approved: 27, rejected: 2 },
  ],
  regionalStats: [
    { region: 'North', bookings: 98, approved: 82, rejected: 16 },
    { region: 'South', bookings: 74, approved: 69, rejected: 5 },
    { region: 'East', bookings: 65, approved: 48, rejected: 17 },
    { region: 'West', bookings: 87, approved: 65, rejected: 22 },
    { region: 'Central', bookings: 122, approved: 110, rejected: 12 },
  ],
  kpis: {
    totalBookings: 346,
    approvalRate: 87.3,
    rejectionRate: 12.7,
    activeJourneys: 14,
    avgDuration: 38,
    cancellations: 23,
  },
};
