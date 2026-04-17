import React, { useEffect, useRef } from 'react';
import { Animated, Easing, View, StyleSheet } from 'react-native';
import Svg, { Path } from 'react-native-svg';
import { useColorScheme } from '@/src/hooks';
import { grays, labels } from '../../../theme/colors';
import type { SpinnerProps } from './types';
import { getStyles } from './styles';

// Spinner arc path (centered at origin for rotation)
const SPINNER_ARC_PATH =
  'M14.79 0C15.4583 0 16.0053 0.5427 15.9496 1.2086C15.7152 4.0104 14.6701 6.6933 12.9293 8.9231C10.9394 11.4719 8.1546 13.2824 5.0177 14.0668C1.8807 14.8513 -1.4284 14.5647 -4.3838 13.2526C-7.3392 11.9405 -9.7712 9.6781 -11.2932 6.8252C-12.8153 3.9723 -13.34 0.6924 -12.7841 -2.493C-12.2281 -5.6784 -10.6234 -8.5866 -8.2249 -10.7553C-5.8265 -12.924 -2.7719 -14.2287 0.4532 -14.4622C3.2747 -14.6664 6.0851 -14.0404 8.5427 -12.6748C9.1269 -12.3502 9.273 -11.5937 8.9009 -11.0386C8.5287 -10.4835 7.7794 -10.3412 7.1899 -10.656C5.1852 -11.7265 2.9109 -12.2137 0.6279 -12.0484C-2.0589 -11.8539 -4.6037 -10.7669 -6.6018 -8.9602C-8.6 -7.1535 -9.9369 -4.7307 -10.4001 -2.0769C-10.8632 0.5769 -10.4261 3.3093 -9.158 5.6861C-7.89 8.0629 -5.8639 9.9476 -3.4018 11.0407C-0.9397 12.1338 1.8172 12.3726 4.4306 11.7191C7.044 11.0655 9.364 9.5572 11.0217 7.4338C12.4303 5.6296 13.2922 3.4693 13.5194 1.208C13.5862 0.5431 14.1217 0 14.79 0Z';

export const Spinner: React.FC<SpinnerProps> = ({ size = 1, style, animated = true }) => {
  const colorScheme = useColorScheme() ?? 'light';
  const styles = getStyles();
  const topSpinValue = useRef(new Animated.Value(0)).current;
  const bottomSpinValue = useRef(new Animated.Value(0)).current;

  // Theme-aware colors
  const topSectionColor = grays.gray6[colorScheme];
  const bottomSectionColor = grays.black[colorScheme];
  const spinnerArcColor = labels.tertiary[colorScheme];

  useEffect(() => {
    if (!animated) return;

    const createSpinAnimation = (value: Animated.Value, duration: number) => {
      return Animated.loop(
        Animated.timing(value, {
          toValue: 1,
          duration,
          easing: Easing.linear,
          useNativeDriver: true,
        })
      );
    };

    const topAnimation = createSpinAnimation(topSpinValue, 1500);
    const bottomAnimation = createSpinAnimation(bottomSpinValue, 1200);

    topAnimation.start();
    bottomAnimation.start();

    return () => {
      topAnimation.stop();
      bottomAnimation.stop();
    };
  }, [animated, topSpinValue, bottomSpinValue]);

  const topSpin = topSpinValue.interpolate({
    inputRange: [0, 1],
    outputRange: ['0deg', '360deg'],
  });

  const bottomSpin = bottomSpinValue.interpolate({
    inputRange: [0, 1],
    outputRange: ['0deg', '360deg'],
  });

  const baseWidth = 112;
  const baseHeight = 284;
  const width = baseWidth * size;
  const height = baseHeight * size;

  // Spinner arc bounding box size
  const arcBoxSize = 32 * size;

  return (
    <View style={[{ width, height }, style]}>
      {/* Background container */}
      <Svg
        width={width}
        height={height}
        viewBox="0 0 112 284"
        fill="none"
        style={StyleSheet.absoluteFill}>
        {/* Bottom section */}
        <Path
          d="M0 141H112V268C112 276.837 104.837 284 96 284H16C7.16344 284 0 276.837 0 268V141Z"
          fill={bottomSectionColor}
        />
        {/* Top section */}
        <Path
          d="M0 16C0 7.16345 7.16344 0 16 0H96C104.837 0 112 7.16344 112 16V141H0V16Z"
          fill={topSectionColor}
        />
      </Svg>

      {/* Top spinner arc - animated */}
      <Animated.View
        style={[
          styles.spinnerArc,
          {
            top: (70.5 - 16) * size,
            left: (56 - 16) * size,
            width: arcBoxSize,
            height: arcBoxSize,
            transform: [{ rotate: topSpin }],
          },
        ]}>
        <Svg width={arcBoxSize} height={arcBoxSize} viewBox="-16 -16 32 32" fill="none">
          <Path d={SPINNER_ARC_PATH} fill={spinnerArcColor} />
        </Svg>
      </Animated.View>

      {/* Bottom spinner arc - animated */}
      <Animated.View
        style={[
          styles.spinnerArc,
          {
            top: (206.5 - 16) * size,
            left: (56 - 16) * size,
            width: arcBoxSize,
            height: arcBoxSize,
            transform: [{ rotate: bottomSpin }],
          },
        ]}>
        <Svg width={arcBoxSize} height={arcBoxSize} viewBox="-16 -16 32 32" fill="none">
          <Path d={SPINNER_ARC_PATH} fill={spinnerArcColor} />
        </Svg>
      </Animated.View>
    </View>
  );
};
