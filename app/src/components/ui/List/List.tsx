import React from 'react';
import { View, Text } from 'react-native';
import { useColorScheme } from '@/src/hooks';
import { ListItem } from '../ListItem';
import type { ListItemPosition } from '../ListItem/types';
import { labels } from '../../../theme/colors';
import type { ListProps } from './types';
import { getStyles } from './styles';

export const List: React.FC<ListProps> = ({
  items,
  header,
  footer,
  style,
  itemsContainerStyle,
}) => {
  const colorScheme = useColorScheme() ?? 'light';
  const styles = getStyles();

  const getPosition = (index: number): ListItemPosition => {
    if (items.length === 1) return 'single';
    if (index === 0) return 'first';
    if (index === items.length - 1) return 'last';
    return 'middle';
  };

  const secondaryTextColor = labels.tertiary[colorScheme];

  return (
    <View style={[styles.container, style]}>
      {header && <Text style={[styles.header, { color: secondaryTextColor }]}>{header}</Text>}
      <View style={[styles.itemsContainer, itemsContainerStyle]}>
        {items.map((item, index) => (
          <ListItem
            key={`list-item-${index}`}
            {...item}
            position={getPosition(index)}
            showSeparator={index < items.length - 1}
          />
        ))}
      </View>
      {footer && <Text style={[styles.footer, { color: secondaryTextColor }]}>{footer}</Text>}
    </View>
  );
};
