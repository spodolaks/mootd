import { useEffect, useState } from 'react';
import NetInfo, { type NetInfoSubscription } from '@react-native-community/netinfo';

/**
 * Connectivity state subscription (mootd#48).
 *
 * Returns the current `isConnected` boolean and a `connectionType`
 * label for callers that want to behave differently on cellular
 * vs wifi (e.g. throttle image preloading on cellular).
 *
 * Initial value is `true` so we don't flash an offline banner
 * on cold-launch before NetInfo has reported. Once the first
 * event lands the real state takes over.
 */
export function useConnectivity(): {
  isConnected: boolean;
  connectionType: string;
} {
  const [isConnected, setIsConnected] = useState(true);
  const [connectionType, setConnectionType] = useState<string>('unknown');

  useEffect(() => {
    let sub: NetInfoSubscription | undefined;
    sub = NetInfo.addEventListener(state => {
      // null → assume connected (dev simulators sometimes report
      // null on first tick); only flip to offline on an explicit
      // false. NetInfo's docs hedge on this — being conservative
      // avoids false-positive "you're offline" banners.
      setIsConnected(state.isConnected !== false);
      setConnectionType(state.type ?? 'unknown');
    });
    return () => {
      if (sub) sub();
    };
  }, []);

  return { isConnected, connectionType };
}
