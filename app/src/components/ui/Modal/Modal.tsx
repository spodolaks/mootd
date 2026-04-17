import React, { useEffect, useRef } from 'react';
import {
  View,
  Text,
  Modal as RNModal,
  Pressable,
  Animated,
  Dimensions,
} from 'react-native';
import { useSafeAreaInsets } from 'react-native-safe-area-context';
import { useColorScheme } from '@/src/hooks';
import { getStyles } from './styles';
import type { ModalProps } from './types';

const { height: SCREEN_HEIGHT } = Dimensions.get('window');

export const Modal: React.FC<ModalProps> = ({
  visible,
  title,
  description,
  buttonLabel,
  onButtonPress,
  onDismiss,
  children,
  showGrabber = true,
  contentStyle,
  ...props
}) => {
  const colorScheme = useColorScheme() ?? 'light';
  const styles = getStyles(colorScheme);
  const insets = useSafeAreaInsets();

  const overlayOpacity = useRef(new Animated.Value(0)).current;
  const slideAnim = useRef(new Animated.Value(SCREEN_HEIGHT)).current;

  useEffect(() => {
    if (visible) {
      Animated.parallel([
        Animated.timing(overlayOpacity, {
          toValue: 1,
          duration: 300,
          useNativeDriver: true,
        }),
        Animated.spring(slideAnim, {
          toValue: 0,
          damping: 20,
          stiffness: 150,
          useNativeDriver: true,
        }),
      ]).start();
    } else {
      Animated.parallel([
        Animated.timing(overlayOpacity, {
          toValue: 0,
          duration: 200,
          useNativeDriver: true,
        }),
        Animated.timing(slideAnim, {
          toValue: SCREEN_HEIGHT,
          duration: 200,
          useNativeDriver: true,
        }),
      ]).start();
    }
  }, [visible, overlayOpacity, slideAnim]);

  const handleOverlayPress = () => {
    onDismiss?.();
  };

  return (
    <RNModal
      visible={visible}
      transparent
      animationType="none"
      onRequestClose={onDismiss}
      statusBarTranslucent
      {...props}>
      <View style={styles.modalWrapper}>
        <Animated.View style={[styles.overlay, { opacity: overlayOpacity }]}>
          <Pressable style={styles.overlayPressable} onPress={handleOverlayPress} />
        </Animated.View>

        <Animated.View style={[styles.container, { transform: [{ translateY: slideAnim }] }]}>
          <Pressable onPress={e => e.stopPropagation()}>
            {/* Header with grabber */}
            {showGrabber && (
              <View style={styles.header}>
                <View style={styles.grabber} />
              </View>
            )}

            {/* Content area */}
            <View style={[styles.content, { paddingBottom: insets.bottom + 24 }, contentStyle]}>
              {/* Title */}
              {title && <Text style={styles.title}>{title}</Text>}

              {/* Description */}
              {description && <Text style={styles.description}>{description}</Text>}

              {/* Custom children */}
              {children}

              {/* Primary button */}
              {buttonLabel && (
                <Pressable style={styles.button} onPress={onButtonPress}>
                  <Text style={styles.buttonText}>{buttonLabel}</Text>
                </Pressable>
              )}
            </View>
          </Pressable>
        </Animated.View>
      </View>
    </RNModal>
  );
};
