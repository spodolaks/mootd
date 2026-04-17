import React from 'react';
import Svg, { Rect, Path } from 'react-native-svg';
import { ViewStyle } from 'react-native';
import { useColorScheme } from '@/src/hooks';
import { grays, labels } from '@/src/theme/colors';

interface NotificationBellProps {
  width?: number;
  height?: number;
  style?: ViewStyle;
}

export function NotificationBell({
  width = 162,
  height = 206,
  style,
}: NotificationBellProps) {
  const colorScheme = useColorScheme() ?? 'light';

  const strokeColor = labels.primary[colorScheme];
  const innerStrokeColor = grays.gray3[colorScheme];
  const fillColor = grays.gray4[colorScheme];

  // Calculate proportions based on original 162x206 dimensions
  const scale = Math.min(width / 162, height / 206);
  const scaledWidth = 162 * scale;
  const scaledHeight = 206 * scale;

  return (
    <Svg width={scaledWidth} height={scaledHeight} viewBox="0 0 162 206" fill="none" style={style}>
      {/* Outer border */}
      <Rect
        x="1"
        y="1"
        width="160"
        height="204"
        rx="22"
        stroke={strokeColor}
        strokeWidth="2"
      />
      {/* Inner border */}
      <Rect
        x="10.5"
        y="11.5"
        width="141"
        height="183"
        rx="14.5"
        stroke={innerStrokeColor}
        strokeWidth="1"
      />
      {/* Bell bottom curve (person silhouette) */}
      <Path
        d="M91.43 131.667C95.467 131.667 97.599 136.445 94.907 139.451C93.157 141.408 91.014 142.974 88.618 144.045C86.221 145.116 83.625 145.669 81 145.667C78.375 145.669 75.779 145.116 73.382 144.045C70.986 142.974 68.843 141.408 67.093 139.451C64.517 136.576 66.356 132.082 70.052 131.699L70.565 131.671L91.43 131.667Z"
        fill={fillColor}
      />
      {/* Bell body */}
      <Path
        d="M81 52.333C87.337 52.333 92.695 56.547 94.417 62.325L94.631 63.123L94.669 63.323C99.813 66.226 104.197 70.305 107.461 75.228C110.725 80.151 112.776 85.777 113.447 91.645L113.578 92.985L113.667 94.333V108.011L113.765 108.646C114.404 112.084 116.306 115.159 119.099 117.265L119.878 117.811L120.634 118.273C124.647 120.546 123.247 126.515 118.875 126.972L118.333 127H43.667C38.869 127 37.194 120.635 41.366 118.273C43.144 117.267 44.68 115.884 45.867 114.221C47.054 112.557 47.862 110.655 48.235 108.646L48.333 107.979L48.338 94.119C48.622 88.023 50.397 82.091 53.505 76.84C56.614 71.59 60.962 67.181 66.169 64L67.327 63.319L67.373 63.118C68.033 60.327 69.534 57.807 71.672 55.896C73.81 53.985 76.482 52.775 79.329 52.431L80.179 52.352L81 52.333Z"
        fill={fillColor}
      />
    </Svg>
  );
}

export default NotificationBell;
