export type JourneyStatus = 'pending' | 'approved' | 'rejected' | 'active' | 'completed' | 'cancelled' | 'expired';
export type VehicleType = 'Car' | 'Van' | 'Motorcycle' | 'HGV';
export type TrafficLevel = 'low' | 'medium' | 'high' | 'critical';
export type Region = 'North' | 'South' | 'East' | 'West' | 'Central';

export interface RouteSegment {
  id: string;
  name: string;
  occupancy: number;
  level: TrafficLevel;
  sequenceOrder?: number;
  traversalMinutes?: number;
  timeWindowStart?: string;
  timeWindowEnd?: string;
  region?: string;
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

export interface CapacitySegment {
  segment_id: string;
  segment_name: string;
  region: string;
  max_capacity: number;
}

export interface SegmentClosure {
  closure_id: string;
  segment_id: string;
  start_time: string;
  end_time: string;
  reason: string;
  admin_id: string;
  status: string;
  created_at: string;
  updated_at: string;
}
