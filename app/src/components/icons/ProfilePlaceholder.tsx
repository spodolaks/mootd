import React from 'react';
import Svg, { Rect, Ellipse, Path } from 'react-native-svg';
import { ViewStyle } from 'react-native';
import { useColorScheme } from '@/src/hooks';
import { grays, labels } from '@/src/theme/colors';

interface ProfilePlaceholderProps {
  width?: number;
  height?: number;
  style?: ViewStyle;
}

export function ProfilePlaceholder({
  width = 162,
  height = 206,
  style,
}: ProfilePlaceholderProps) {
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
      {/* Head ellipse */}
      <Ellipse
        cx="81"
        cy="57"
        rx="21.5"
        ry="21"
        fill={fillColor}
      />
      {/* Body shape */}
      <Path
        d="M52.538 97.426C59.373 86.497 101.627 85.89 108.462 97.426C115.297 108.962 108.462 163 108.462 163H52.538C52.538 163 45.703 108.355 52.538 97.426Z"
        fill={fillColor}
      />
    </Svg>
  );
}

export default ProfilePlaceholder;
