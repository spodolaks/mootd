import { useLocalSearchParams, useRouter } from 'expo-router';
import React, { useState } from 'react';
import {
  ActivityIndicator,
  KeyboardAvoidingView,
  Platform,
  ScrollView,
  StyleSheet,
  Text,
  Pressable,
  View,
} from 'react-native';
import { Image } from 'expo-image';
import { SafeAreaView } from 'react-native-safe-area-context';

import { GradientButton, Icon, Input, Modal } from '@/src/components';
import { useColorScheme } from '@/src/hooks';
import { accents, backgrounds, button, fills, grays, labels } from '@/src/theme/colors';
import { typography } from '@/src/theme/typography';
import { wardrobeRepository } from '@/src/data/repositories';

// Fixed trait keys shown in the editor. Values come from the item's traits map.
const TRAIT_KEYS: { key: string; label: string }[] = [
  { key: 'color', label: 'Color' },
  { key: 'material', label: 'Material' },
  { key: 'size', label: 'Size' },
  { key: 'brand', label: 'Brand' },
  { key: 'occasion', label: 'Occasion' },
];

export const ItemDetailsScreen: React.FC = () => {
  const colorScheme = useColorScheme() ?? 'light';
  const router = useRouter();
  const params = useLocalSearchParams<{
    id: string;
    name: string;
    category: string;
    imageUrl: string;
    traits: string;
  }>();

  const itemId = params.id ?? '';
  const itemName = params.name || 'Item';
  const itemCategory = params.category || '';
  const itemImageUrl = params.imageUrl || '';

  const initialTraits: Record<string, string> = (() => {
    try {
      return params.traits ? (JSON.parse(params.traits) as Record<string, string>) : {};
    } catch {
      return {};
    }
  })();

  const [traitValues, setTraitValues] = useState<Record<string, string>>(initialTraits);
  const [isSaving, setIsSaving] = useState(false);
  const [isDeleting, setIsDeleting] = useState(false);
  const [showDeleteModal, setShowDeleteModal] = useState(false);

  const backgroundColor = backgrounds.primary[colorScheme];
  const textColor = labels.primary[colorScheme];
  const secondaryTextColor = labels.secondary[colorScheme];
  const imagePlaceholderBg = grays.gray4[colorScheme];
  const placeholderColor = labels.tertiary[colorScheme];
  const destructiveColor = accents.red[colorScheme];
  const chipBg = fills.tertiary[colorScheme];
  const cancelBg = button.secondary.background[colorScheme];
  const cancelText = button.secondary.foreground[colorScheme];

  const handleTraitChange = (key: string, value: string) => {
    setTraitValues(prev => ({ ...prev, [key]: value }));
  };

  const handleDeleteConfirm = async () => {
    setShowDeleteModal(false);
    setIsDeleting(true);
    try {
      await wardrobeRepository.deleteItem(itemId);
      router.back();
    } catch (e) {
      console.error('Delete failed:', e);
    } finally {
      setIsDeleting(false);
    }
  };

  const handleSave = async () => {
    setIsSaving(true);
    try {
      const filtered = Object.fromEntries(
        Object.entries(traitValues).filter(([, v]) => v.trim() !== '')
      );
      await wardrobeRepository.updateItem(itemId, filtered);
      router.back();
    } catch (e) {
      console.error('Save failed:', e);
    } finally {
      setIsSaving(false);
    }
  };

  return (
    <SafeAreaView style={[styles.container, { backgroundColor }]} edges={['top']}>
      <KeyboardAvoidingView
        behavior={Platform.OS === 'ios' ? 'padding' : 'height'}
        style={styles.keyboardAvoid}>
        {/* Header */}
        <View style={styles.header}>
          <Pressable
            style={styles.backButton}
            onPress={() => router.back()}
            hitSlop={8}
            accessibilityRole="button"
            accessibilityLabel="Go back">
            <Icon name="chevron-left" size={24} color={textColor} />
          </Pressable>
          <Text style={[styles.title, { color: textColor }]} numberOfLines={1}>
            {itemName}
          </Text>
          <Pressable
            style={styles.deleteButton}
            onPress={() => setShowDeleteModal(true)}
            disabled={isDeleting}
            hitSlop={8}
            accessibilityRole="button"
            accessibilityLabel="Delete item"
            accessibilityState={{ disabled: isDeleting }}>
            <Icon name="bin" size={22} color={isDeleting ? placeholderColor : destructiveColor} />
          </Pressable>
        </View>

        <ScrollView
          style={styles.scrollView}
          contentContainerStyle={styles.scrollContent}
          showsVerticalScrollIndicator={false}
          keyboardShouldPersistTaps="handled">
          {/* Item image */}
          <View style={[styles.imageContainer, { backgroundColor: imagePlaceholderBg }]}>
            {itemImageUrl ? (
              <Image
                source={{ uri: itemImageUrl }}
                style={styles.image}
                contentFit="contain"
                cachePolicy="memory-disk"
              />
            ) : (
              <View style={styles.imagePlaceholder}>
                <Icon name="closet" size={48} color={placeholderColor} />
              </View>
            )}
          </View>

          {/* Category chip */}
          {itemCategory ? (
            <View style={styles.categoryRow}>
              <View style={[styles.categoryChip, { backgroundColor: chipBg }]}>
                <Text style={[styles.categoryText, { color: secondaryTextColor }]}>
                  {itemCategory}
                </Text>
              </View>
            </View>
          ) : null}

          {/* Trait fields */}
          <View style={styles.traitsContainer}>
            {TRAIT_KEYS.map(({ key, label }) => (
              <Input
                key={key}
                title={label}
                placeholder={`Enter ${label.toLowerCase()}`}
                value={traitValues[key] ?? ''}
                onChangeText={text => handleTraitChange(key, text)}
              />
            ))}
          </View>
        </ScrollView>

        {/* Save button */}
        <View style={styles.buttonContainer}>
          {isSaving ? (
            <ActivityIndicator color={button.primary.background[colorScheme]} />
          ) : (
            <GradientButton
              label="Save"
              onPress={() => {
                void handleSave();
              }}
              testID="item-details-save"
              accessibilityLabel="Save changes to this item"
            />
          )}
        </View>
      </KeyboardAvoidingView>

      {/* Delete confirmation modal */}
      <Modal
        visible={showDeleteModal}
        title="Remove item"
        description="Are you sure you want to permanently remove this item from your wardrobe?"
        onDismiss={() => setShowDeleteModal(false)}
        showGrabber={false}>
        <View style={styles.modalButtons}>
          <Pressable
            style={[styles.modalButton, { backgroundColor: destructiveColor }]}
            onPress={() => {
              void handleDeleteConfirm();
            }}
            accessibilityRole="button"
            accessibilityLabel="Confirm remove">
            <Text style={[styles.modalButtonText, { color: '#FFFFFF' }]}>Remove</Text>
          </Pressable>
          <Pressable
            style={[styles.modalButton, { backgroundColor: cancelBg }]}
            onPress={() => setShowDeleteModal(false)}
            accessibilityRole="button"
            accessibilityLabel="Cancel">
            <Text style={[styles.modalButtonText, { color: cancelText }]}>Cancel</Text>
          </Pressable>
        </View>
      </Modal>
    </SafeAreaView>
  );
};

const styles = StyleSheet.create({
  container: { flex: 1 },
  keyboardAvoid: { flex: 1 },
  header: {
    flexDirection: 'row',
    alignItems: 'center',
    paddingHorizontal: 16,
    paddingVertical: 12,
    gap: 8,
  },
  backButton: {
    width: 40,
    height: 40,
    justifyContent: 'center',
    alignItems: 'flex-start',
  },
  title: {
    flex: 1,
    ...typography.largeTitle.semiBold,
  },
  deleteButton: { width: 40, height: 40, justifyContent: 'center', alignItems: 'flex-end' },
  scrollView: { flex: 1 },
  scrollContent: {
    paddingHorizontal: 16,
    paddingBottom: 24,
  },
  imageContainer: {
    width: '100%',
    aspectRatio: 343 / 208,
    borderRadius: 24,
    marginBottom: 16,
    overflow: 'hidden',
  },
  image: { width: '100%', height: '100%' },
  imagePlaceholder: {
    flex: 1,
    justifyContent: 'center',
    alignItems: 'center',
  },
  categoryRow: {
    flexDirection: 'row',
    marginBottom: 20,
  },
  categoryChip: {
    paddingHorizontal: 12,
    paddingVertical: 4,
    borderRadius: 12,
  },
  categoryText: {
    ...typography.footnote.semiBold,
    textTransform: 'capitalize',
  },
  traitsContainer: { gap: 4 },
  buttonContainer: {
    paddingHorizontal: 16,
    paddingBottom: 16,
    paddingTop: 8,
  },
  modalButtons: {
    gap: 8,
  },
  modalButton: {
    height: 54,
    borderRadius: 27,
    justifyContent: 'center',
    alignItems: 'center',
  },
  modalButtonText: {
    ...typography.body.semiBold,
  },
});
