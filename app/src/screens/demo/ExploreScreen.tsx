import React, { useState } from 'react';
import { View, StyleSheet, ImageBackground } from 'react-native';
import { SafeAreaView } from 'react-native-safe-area-context';
import { LinearGradient } from 'expo-linear-gradient';
import { useColorScheme, useWeather } from '@/src/hooks';
import {
  WeatherCard,
  GradientButton,
  GradientIconButton,
  TabBar,
  Modal,
  Button,
} from '@/src/components';
import { backgrounds } from '@/src/theme/colors';

export const ExploreScreen: React.FC = () => {
  const colorScheme = useColorScheme() ?? 'light';
  const [selectedTab, setSelectedTab] = useState('home');
  const [isAddModalVisible, setIsAddModalVisible] = useState(false);

  const backgroundColor = backgrounds.primary[colorScheme];
  const { weather, refresh: refreshWeather } = useWeather();

  const tabs = [
    { id: 'home', label: 'Home', icon: 'home' as const },
    { id: 'closet', label: 'Closet', icon: 'closet' as const },
  ];

  const handleResetPress = () => {
    // TODO: implement
  };

  const handleAddPress = () => {
    setIsAddModalVisible(true);
  };

  const handleCloseAddModal = () => {
    setIsAddModalVisible(false);
  };

  const handleCameraPress = () => {
    // TODO: implement
    setIsAddModalVisible(false);
  };

  const handleAlbumPress = () => {
    // TODO: implement
    setIsAddModalVisible(false);
  };

  const handleUploadPress = () => {
    // TODO: implement
    setIsAddModalVisible(false);
  };

  const handleTabPress = (tab: { id: string; label: string }) => {
    setSelectedTab(tab.id);
  };

  return (
    <SafeAreaView style={[styles.container, { backgroundColor }]} edges={['top']}>
      <View style={styles.content}>
        {/* Weather Card */}
        {weather && (
          <WeatherCard
            temperature={weather.temperature}
            unit={weather.unit}
            condition={weather.condition}
            icon={weather.icon}
            lowTemperature={weather.lowTemperature}
            highTemperature={weather.highTemperature}
            location={weather.location}
            onLocationPress={refreshWeather}
          />
        )}

        {/* Music/Media Card */}
        <View style={styles.musicCardContainer}>
          <ImageBackground
            source={require('@/assets/images/moodb.png')}
            style={styles.musicCard}
            imageStyle={styles.musicCardImage}
            resizeMode="cover">
            {/* Gradient overlay at bottom */}
            <LinearGradient
              colors={['transparent', 'rgba(0,0,0,0.3)']}
              style={styles.musicCardGradient}
            />

            {/* Reset Button */}
            <GradientIconButton
              icon="reset"
              size="md"
              onPress={handleResetPress}
              style={styles.resetButton}
            />
          </ImageBackground>
        </View>

        {/* Add Button */}
        <GradientButton label="Add" icon="plus" onPress={handleAddPress} />
      </View>

      {/* Bottom Tab Bar */}
      <TabBar tabs={tabs} selectedId={selectedTab} onTabPress={handleTabPress} />

      {/* Add Item Modal */}
      <Modal visible={isAddModalVisible} onDismiss={handleCloseAddModal} title="Add item">
        <View style={styles.modalButtons}>
          <Button
            label="Camera"
            icon="camera"
            variant="primary"
            size="lg"
            onPress={handleCameraPress}
            style={styles.modalButton}
          />
          <Button
            label="Album"
            icon="image"
            variant="secondary"
            size="lg"
            onPress={handleAlbumPress}
            style={styles.modalButton}
          />
          <Button
            label="Upload"
            icon="upload"
            variant="secondary"
            size="lg"
            onPress={handleUploadPress}
            style={styles.modalButton}
          />
        </View>
      </Modal>
    </SafeAreaView>
  );
};

const styles = StyleSheet.create({
  container: {
    flex: 1,
  },
  content: {
    flex: 1,
    paddingHorizontal: 15,
    paddingBottom: 15,
    gap: 16,
  },
  gradientButton: {
    marginBottom: 29,
  },
  musicCardContainer: {
    flex: 1,
    borderRadius: 24,
    overflow: 'hidden',
    // Shadow for iOS
    shadowColor: '#000',
    shadowOffset: {
      width: 0,
      height: 4,
    },
    shadowOpacity: 0.15,
    shadowRadius: 12,
    // Elevation for Android
    elevation: 8,
  },
  musicCard: {
    flex: 1,
    backgroundColor: '#E5E5EA',
    justifyContent: 'flex-end',
  },
  musicCardImage: {
    borderRadius: 24,
  },
  musicCardGradient: {
    position: 'absolute',
    bottom: 0,
    left: 0,
    right: 0,
    height: 100,
    borderBottomLeftRadius: 24,
    borderBottomRightRadius: 24,
  },
  resetButton: {
    position: 'absolute',
    top: 18,
    right: 14,
  },
  modalButtons: {
    gap: 12,
  },
  modalButton: {
    width: '100%',
  },
});
