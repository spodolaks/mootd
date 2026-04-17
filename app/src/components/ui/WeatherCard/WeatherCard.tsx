import React from 'react';
import { View, Text } from 'react-native';
import { useColorScheme } from '@/src/hooks';
import { Icon } from '../../icons';
import { getStyles } from './styles';
import { labels } from '../../../theme/colors';
import type { WeatherCardProps } from './types';

export const WeatherCard: React.FC<WeatherCardProps> = ({
  temperature,
  unit = 'c',
  condition,
  lowTemperature,
  highTemperature,
  location,
  icon = 'cloud',
  onLocationPress,
  style,
}) => {
  const colorScheme = useColorScheme() ?? 'light';
  const styles = getStyles(colorScheme);

  const iconColor = labels.primary[colorScheme];

  return (
    <View style={[styles.container, style]}>
      {/* Left section: weather info */}
      <View style={styles.leftSection}>
        {/* Top row: cloud icon and temperature */}
        <View style={styles.topRow}>
          <View style={styles.iconContainer}>
            <Icon name={icon} size={32} color={iconColor} />
          </View>

          <View style={styles.temperatureRow}>
            <Text style={styles.temperature}>{temperature}</Text>
            <View style={styles.degreeUnitColumn}>
              <Text style={styles.degreeSymbol}>°</Text>
              <Text style={styles.unitText}>{unit}</Text>
            </View>
          </View>
        </View>

        {/* Bottom row: condition and high/low */}
        <View style={styles.bottomRow}>
          <Text style={styles.condition}>{condition}</Text>
          <Text style={styles.highLowText}>
            L{lowTemperature}°, H{highTemperature}°
          </Text>
        </View>
      </View>

      {/* Right section: location (centered vertically) */}
      <View style={styles.rightSection}>
        <Text style={styles.location}>{location}</Text>
      </View>
    </View>
  );
};
