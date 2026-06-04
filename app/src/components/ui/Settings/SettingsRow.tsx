/**
 * Reusable, data-driven settings row component.
 *
 * Supports four interaction modes:
 *  • navigation  – tappable row with chevron, fires onPress
 *  • toggle      – Switch on the right, fires onValueChange
 *  • select      – tappable row with checkmark when selected
 *  • display     – non-interactive info row (e.g. email)
 *
 * All interactive rows use Pressable (web-safe) and icons use
 * pointerEvents="none" so taps always pass through.
 */

import React from 'react';
import { Pressable, StyleSheet, Switch, Text, View } from 'react-native';
import { Icon } from '../../icons';
import type { IconName } from '../../icons';

// ─── Types ───────────────────────────────────────────────────────────────────

interface BaseProps {
  icon?: IconName;
  label: string;
  /** Optional secondary text shown below the label. */
  subtitle?: string;
  textColor: string;
  subtitleColor?: string;
  dividerColor?: string;
  showDivider?: boolean;
}

interface NavigationRowProps extends BaseProps {
  mode: 'navigation';
  onPress: () => void;
  chevronColor?: string;
}

interface ToggleRowProps extends BaseProps {
  mode: 'toggle';
  value: boolean;
  onValueChange: (next: boolean) => void;
}

interface SelectRowProps extends BaseProps {
  mode: 'select';
  selected: boolean;
  onPress: () => void;
  accentColor?: string;
}

interface DisplayRowProps extends BaseProps {
  mode: 'display';
}

export type SettingsRowProps =
  | NavigationRowProps
  | ToggleRowProps
  | SelectRowProps
  | DisplayRowProps;

// ─── Component ───────────────────────────────────────────────────────────────

export const SettingsRow: React.FC<SettingsRowProps> = props => {
  const {
    icon,
    label,
    subtitle,
    textColor,
    subtitleColor,
    dividerColor,
    showDivider = false,
    mode,
  } = props;

  const content = (
    <View style={styles.row}>
      {icon && <Icon name={icon} size={20} color={textColor} />}
      <View style={styles.labelContainer}>
        <Text style={[styles.label, { color: textColor }]}>{label}</Text>
        {subtitle != null && (
          <Text style={[styles.subtitle, { color: subtitleColor ?? textColor }]}>{subtitle}</Text>
        )}
      </View>

      {mode === 'toggle' && <Switch value={props.value} onValueChange={props.onValueChange} />}
      {mode === 'select' && props.selected && (
        <Icon name="check" size={18} color={props.accentColor ?? '#007AFF'} />
      )}
      {mode === 'navigation' && (
        <Icon name="chevron-right" size={16} color={props.chevronColor ?? textColor} />
      )}
    </View>
  );

  const isInteractive = mode !== 'display' && mode !== 'toggle';

  return (
    <>
      {isInteractive ? (
        <Pressable
          onPress={(props as NavigationRowProps | SelectRowProps).onPress}
          style={({ pressed }) => (pressed ? { opacity: 0.7 } : undefined)}>
          {content}
        </Pressable>
      ) : (
        content
      )}
      {showDivider && dividerColor && (
        <View style={[styles.divider, { backgroundColor: dividerColor }]} />
      )}
    </>
  );
};

// ─── Section header companion ────────────────────────────────────────────────

export const SettingsSection: React.FC<{
  title: string;
  color: string;
  children: React.ReactNode;
  cardBackground: string;
}> = ({ title, color, children, cardBackground }) => (
  <>
    <Text style={[styles.sectionTitle, { color }]}>{title}</Text>
    <View style={[styles.card, { backgroundColor: cardBackground }]}>{children}</View>
  </>
);

// ─── Styles ──────────────────────────────────────────────────────────────────

const styles = StyleSheet.create({
  row: {
    flexDirection: 'row',
    alignItems: 'center',
    paddingHorizontal: 16,
    paddingVertical: 14,
    gap: 12,
  },
  labelContainer: {
    flex: 1,
  },
  label: {
    fontSize: 17,
    lineHeight: 22,
  },
  subtitle: {
    fontSize: 13,
    lineHeight: 18,
    marginTop: 2,
  },
  divider: {
    height: StyleSheet.hairlineWidth,
    marginLeft: 48,
  },
  sectionTitle: {
    fontSize: 13,
    fontWeight: '600',
    textTransform: 'uppercase',
    marginTop: 24,
    marginBottom: 8,
    marginLeft: 4,
  },
  card: {
    borderRadius: 16,
    overflow: 'hidden',
  },
});
