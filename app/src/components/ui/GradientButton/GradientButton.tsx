import React, { useId } from 'react';
import { Text, Pressable, View, useWindowDimensions } from 'react-native';
import Svg, {
  Defs,
  Ellipse,
  Rect,
  Stop,
  LinearGradient as SvgLinearGradient,
} from 'react-native-svg';
import { useColorScheme } from '../../../hooks/useColorScheme';
import { button, gradients, grays } from '../../../theme/colors';
import { Icon } from '../../icons';
import { BORDER_RADIUS, BUTTON_HEIGHT, getStyles } from './styles';
import type { GradientButtonProps } from './types';

// Light theme background gradient colors (dark gradient)
const LIGHT_BG_GRADIENT = {
  start: grays.gray4.dark, // #3A3A3C
  end: grays.black.light, // #000000
};

export const GradientButton: React.FC<GradientButtonProps> = ({
  label,
  icon,
  iconPosition = 'left',
  disabled = false,
  style,
  variant = 'solid',
  borderGradientColors = [...gradients.primary.colors],
  backgroundGradientColors,
  ...touchableProps
}) => {
  const isIconOnly = !label && !!icon;
  const { width: screenWidth } = useWindowDimensions();
  // Guard against 0 on first web render (useWindowDimensions can return 0
  // before the layout is measured). Fall back to a sensible minimum so the
  // button is always tappable.
  const buttonWidth = Math.max(screenWidth - 32, 100);
  const colorScheme = useColorScheme();
  const styles = getStyles(colorScheme);
  // Per-instance prefix so SVG `url(#…)` refs can't collide with another
  // instance's <defs> on the same DOM (Expo web renders react-native-svg
  // to real SVG; gradient IDs are document-wide there, and a previously
  // mounted button's `bgGradient` would shadow a later button's fill —
  // surfacing as an invisible button in light theme where the fill is a
  // gradient ref instead of a solid color).
  const uid = useId().replace(/:/g, '');
  const ids = {
    bg: `gb-bg-${uid}`,
    border: `gb-border-${uid}`,
    fade: `gb-fade-${uid}`,
    glow: `gb-glow-${uid}`,
  };

  // Theme-aware colors using design tokens
  const isLightTheme = colorScheme === 'light';
  const textColor = button.primary.foreground[colorScheme];
  // Fade color for the top fade effect
  const fadeColor = isLightTheme ? grays.gray3.dark : grays.gray3.light;
  // Glow gradient colors from the design system
  const glowColors = gradients.primary.colors;
  // Dark theme uses solid white background
  const darkBgColor = button.primary.background.dark;

  // Glow component - simulated blur with stacked ellipses
  const ShadowGlow = () => (
    <View style={styles.glowContainer}>
      <Svg width="100%" height="100%" viewBox="0 0 375 50" preserveAspectRatio="xMidYMid slice">
        <Defs>
          <SvgLinearGradient
            id={ids.glow}
            x1={15}
            y1={25}
            x2={360}
            y2={25}
            gradientUnits="userSpaceOnUse">
            <Stop stopColor={glowColors[0]} />
            <Stop offset={0.336538} stopColor={glowColors[1]} />
            <Stop offset={0.644231} stopColor={glowColors[2]} />
            <Stop offset={1} stopColor={glowColors[3]} />
          </SvgLinearGradient>
        </Defs>
        <Ellipse cx={187.5} cy={25} rx={172.5} ry={25} fill={`url(#${ids.glow})`} opacity={0.01} />
        <Ellipse cx={187.5} cy={25} rx={172.5} ry={22} fill={`url(#${ids.glow})`} opacity={0.015} />
        <Ellipse cx={187.5} cy={25} rx={170} ry={19} fill={`url(#${ids.glow})`} opacity={0.02} />
        <Ellipse cx={187.5} cy={25} rx={167} ry={16} fill={`url(#${ids.glow})`} opacity={0.025} />
        <Ellipse cx={187.5} cy={25} rx={164} ry={13} fill={`url(#${ids.glow})`} opacity={0.03} />
        <Ellipse cx={187.5} cy={25} rx={160} ry={10} fill={`url(#${ids.glow})`} opacity={0.035} />
        <Ellipse cx={187.5} cy={25} rx={156} ry={8} fill={`url(#${ids.glow})`} opacity={0.04} />
        <Ellipse cx={187.5} cy={25} rx={152} ry={6} fill={`url(#${ids.glow})`} opacity={0.045} />
        <Ellipse cx={187.5} cy={25} rx={148} ry={5} fill={`url(#${ids.glow})`} opacity={0.05} />
      </Svg>
    </View>
  );

  // Button SVG with background, border, and fade
  const ButtonSvg = () => (
    <Svg width={buttonWidth} height={BUTTON_HEIGHT}>
      <Defs>
        {/* Background gradient for light theme - diagonal dark gradient */}
        {isLightTheme && (
          <SvgLinearGradient
            id={ids.bg}
            x1="0"
            y1="0"
            x2={buttonWidth * 0.35}
            y2={BUTTON_HEIGHT * 3.5}
            gradientUnits="userSpaceOnUse">
            <Stop offset="0" stopColor={LIGHT_BG_GRADIENT.start} />
            <Stop offset="1" stopColor={LIGHT_BG_GRADIENT.end} />
          </SvgLinearGradient>
        )}

        {/* Border gradient - horizontal colorful */}
        <SvgLinearGradient
          id={ids.border}
          x1="0"
          y1={BUTTON_HEIGHT / 2}
          x2={buttonWidth}
          y2={BUTTON_HEIGHT / 2}
          gradientUnits="userSpaceOnUse">
          <Stop offset="0" stopColor={glowColors[0]} />
          <Stop offset={0.336538} stopColor={glowColors[1]} />
          <Stop offset={0.644231} stopColor={glowColors[2]} />
          <Stop offset="1" stopColor={glowColors[3]} />
        </SvgLinearGradient>

        {/* Top fade gradient - vertical */}
        <SvgLinearGradient
          id={ids.fade}
          x1={buttonWidth / 2}
          y1="0"
          x2={buttonWidth / 2}
          y2={BUTTON_HEIGHT}
          gradientUnits="userSpaceOnUse">
          <Stop offset="0" stopColor={fadeColor} />
          <Stop offset="0.5" stopColor={fadeColor} stopOpacity={0} />
        </SvgLinearGradient>
      </Defs>

      {/* Background fill - gradient for light, solid for dark */}
      <Rect
        x={1}
        y={1}
        width={buttonWidth - 2}
        height={BUTTON_HEIGHT - 2}
        rx={BORDER_RADIUS - 1}
        fill={isLightTheme ? `url(#${ids.bg})` : darkBgColor}
      />

      {/* Colorful border */}
      <Rect
        x={1}
        y={1}
        width={buttonWidth - 2}
        height={BUTTON_HEIGHT - 2}
        rx={BORDER_RADIUS - 1}
        stroke={`url(#${ids.border})`}
        strokeOpacity={0.8}
        strokeWidth={2}
        fill="none"
      />

      {/* Top fade stroke */}
      <Rect
        x={1}
        y={1}
        width={buttonWidth - 2}
        height={BUTTON_HEIGHT - 2}
        rx={BORDER_RADIUS - 1}
        stroke={`url(#${ids.fade})`}
        strokeWidth={2}
        fill="none"
      />
    </Svg>
  );

  const renderContent = () => {
    if (isIconOnly) {
      return <Icon name={icon!} size={24} color={textColor} />;
    }

    return (
      <>
        {icon && iconPosition === 'left' && (
          <Icon name={icon} size={24} color={textColor} style={styles.iconLeft} />
        )}
        {label && <Text style={styles.label}>{label}</Text>}
        {icon && iconPosition === 'right' && (
          <Icon name={icon} size={24} color={textColor} style={styles.iconRight} />
        )}
      </>
    );
  };

  return (
    <View style={[styles.wrapper, disabled && styles.disabled, style]}>
      <ShadowGlow />
      <Pressable disabled={disabled} style={styles.touchable} {...touchableProps}>
        <ButtonSvg />
        <View style={styles.content}>{renderContent()}</View>
      </Pressable>
    </View>
  );
};
