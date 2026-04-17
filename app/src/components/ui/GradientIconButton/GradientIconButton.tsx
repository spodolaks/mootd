import React from 'react';
import { Pressable, View } from 'react-native';
import Svg, { Circle, Defs, LinearGradient as SvgLinearGradient, Stop } from 'react-native-svg';
import { gradients, grays } from '../../../theme/colors';
import { spacing } from '../../../theme/spacing';
import { Icon } from '../../icons';
import type { GradientIconButtonProps } from './types';
import { getStyles, SIZES } from './styles';

export const GradientIconButton: React.FC<GradientIconButtonProps> = ({
  icon,
  size = 'md',
  disabled = false,
  style,
  borderGradientColors = [...gradients.primary.colors],
  ...touchableProps
}) => {
  const styles = getStyles();
  const { dimension, radius } = SIZES[size];

  // Center point for the circle
  const center = dimension / 2;

  // Button SVG with full gradient background
  const ButtonSvg = () => (
    <Svg width={dimension} height={dimension}>
      <Defs>
        {/* Full gradient background - diagonal colorful */}
        <SvgLinearGradient
          id="bgGradient"
          x1="0"
          y1="0"
          x2={dimension}
          y2={dimension}
          gradientUnits="userSpaceOnUse">
          <Stop offset="0" stopColor={borderGradientColors[0]} />
          <Stop offset="0.336538" stopColor={borderGradientColors[1]} />
          <Stop offset="0.644231" stopColor={borderGradientColors[2]} />
          <Stop offset="1" stopColor={borderGradientColors[3]} />
        </SvgLinearGradient>
      </Defs>

      {/* Background fill circle with gradient */}
      <Circle cx={center} cy={center} r={radius} fill="url(#bgGradient)" />
    </Svg>
  );

  return (
    <View style={[styles.wrapper, disabled && styles.disabled, style]}>
      <Pressable
        disabled={disabled}
        style={[styles.touchable, { width: dimension, height: dimension }]}
        {...touchableProps}>
        <ButtonSvg />
        <View style={styles.content}>
          <Icon name={icon} size={spacing.lg} color={grays.white.light} />
        </View>
      </Pressable>
    </View>
  );
};
