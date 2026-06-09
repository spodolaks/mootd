import React from 'react';
import { ActivityIndicator, Pressable, ScrollView, StyleSheet, View } from 'react-native';
import { SafeAreaView } from 'react-native-safe-area-context';
import { useRouter } from 'expo-router';
import { Icon, Text } from '@/src/components';
import { useColorScheme } from '@/src/hooks';
import { useDetectionJobStore, useUIStore, type DetectionJob } from '@/src/store';
import { accents, backgrounds, fills, grays, labels } from '@/src/theme/colors';
import { typography } from '@/src/theme/typography';
import { spacing } from '@/src/theme/spacing';
import { radius } from '@/src/theme/radius';

const formatElapsed = (startedAt: number, completedAt: number | null): string => {
  const end = completedAt ?? Date.now();
  const seconds = Math.floor((end - startedAt) / 1000);
  if (seconds < 60) return `${seconds}s`;
  const minutes = Math.floor(seconds / 60);
  const remainingSeconds = seconds % 60;
  return `${minutes}m ${remainingSeconds}s`;
};

const JobCard: React.FC<{
  job: DetectionJob;
  colorScheme: 'light' | 'dark';
  onDismiss: (id: string) => void;
  onRetry: (job: DetectionJob) => void;
}> = ({ job, colorScheme, onDismiss, onRetry }) => {
  const cardBg = grays.gray5[colorScheme];
  const textColor = labels.primary[colorScheme];
  const secondaryText = labels.secondary[colorScheme];
  const tertiaryText = labels.tertiary[colorScheme];
  const accentBlue = accents.blue[colorScheme];

  const statusColor =
    job.status === 'completed'
      ? accents.green[colorScheme]
      : job.status === 'failed'
        ? accents.red[colorScheme]
        : accents.blue[colorScheme];

  return (
    <View style={[styles.jobCard, { backgroundColor: cardBg }]}>
      <View style={styles.jobHeader}>
        <View style={styles.jobStatus}>
          {job.status === 'detecting' ? (
            <ActivityIndicator size="small" color={statusColor} />
          ) : (
            <Icon
              name={job.status === 'completed' ? 'check' : 'alert'}
              size={18}
              color={statusColor}
            />
          )}
          <Text variant="subheadline" weight="semiBold" style={{ color: textColor }}>
            {job.status === 'detecting'
              ? 'Detecting...'
              : job.status === 'completed'
                ? 'Complete'
                : 'Failed'}
          </Text>
        </View>
        {job.status !== 'detecting' && (
          <View style={styles.jobActions}>
            {job.status === 'failed' && (
              <Pressable
                onPress={() => onRetry(job)}
                hitSlop={8}
                style={styles.retryButton}
                testID={`detection-retry-${job.id}`}
                accessibilityRole="button"
                accessibilityLabel="Retry detection for this photo">
                <Icon name="reset" size={14} color={accentBlue} />
                <Text variant="footnote" weight="semiBold" style={{ color: accentBlue }}>
                  Retry
                </Text>
              </Pressable>
            )}
            <Pressable
              onPress={() => onDismiss(job.id)}
              hitSlop={8}
              testID={`detection-dismiss-${job.id}`}
              accessibilityRole="button"
              accessibilityLabel="Dismiss this detection job">
              <Icon name="close" size={16} color={tertiaryText} />
            </Pressable>
          </View>
        )}
      </View>

      <Text variant="footnote" style={[styles.statusText, { color: secondaryText }]}>
        {job.statusText}
      </Text>

      <View style={styles.jobFooter}>
        <Text variant="caption1" style={{ color: tertiaryText }}>
          {job.status === 'detecting' ? 'Elapsed' : 'Took'}{' '}
          {formatElapsed(job.startedAt, job.completedAt)}
        </Text>
        {job.result && (
          <Text variant="caption1" style={{ color: tertiaryText }}>
            {job.result.items.length} item{job.result.items.length === 1 ? '' : 's'} detected
          </Text>
        )}
      </View>

      {job.status === 'detecting' && (
        <View style={styles.progressContainer}>
          <View style={[styles.progressTrack, { backgroundColor: fills.tertiary[colorScheme] }]}>
            <View style={[styles.progressIndeterminate, { backgroundColor: statusColor }]} />
          </View>
        </View>
      )}
    </View>
  );
};

export const DetectionActivityScreen: React.FC = () => {
  const colorScheme = useColorScheme() ?? 'light';
  const router = useRouter();
  const jobs = useDetectionJobStore(s => s.jobs);
  const dismissJob = useDetectionJobStore(s => s.dismissJob);
  const startJob = useDetectionJobStore(s => s.startJob);
  const showToast = useUIStore(s => s.showToast);

  // Re-run detection from the failed job's stored image. Same path the
  // Wardrobe screen uses (startJob + a "detection started" toast); the old
  // failed entry is removed so it doesn't pile up below the fresh one.
  const handleRetry = React.useCallback(
    (job: DetectionJob) => {
      startJob(job.imageUri);
      dismissJob(job.id);
      showToast('Detection restarted — you can keep browsing', 'info');
    },
    [startJob, dismissJob, showToast]
  );

  const backgroundColor = backgrounds.primary[colorScheme];
  const textColor = labels.primary[colorScheme];
  const secondaryText = labels.secondary[colorScheme];

  return (
    <SafeAreaView style={[styles.container, { backgroundColor }]} edges={['top']}>
      <View style={styles.header}>
        <Pressable onPress={() => router.back()} hitSlop={8}>
          <Icon name="chevron-left" size={24} color={textColor} />
        </Pressable>
        <Text style={[styles.title, { color: textColor }]}>Detection Activity</Text>
        <View style={{ width: 24 }} />
      </View>

      <ScrollView
        style={styles.scrollView}
        contentContainerStyle={styles.scrollContent}
        showsVerticalScrollIndicator={false}>
        {jobs.length === 0 ? (
          <View style={styles.emptyState}>
            <Icon name="camera" size={48} color={secondaryText} />
            <Text variant="subheadline" style={[styles.emptyText, { color: secondaryText }]}>
              No detection activity yet.{'\n'}Upload a photo from the Wardrobe tab to get started.
            </Text>
          </View>
        ) : (
          jobs.map(job => (
            <JobCard
              key={job.id}
              job={job}
              colorScheme={colorScheme}
              onDismiss={dismissJob}
              onRetry={handleRetry}
            />
          ))
        )}
      </ScrollView>
    </SafeAreaView>
  );
};

const styles = StyleSheet.create({
  container: {
    flex: 1,
  },
  header: {
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'space-between',
    paddingHorizontal: spacing.md,
    paddingTop: spacing.sm,
    paddingBottom: spacing.md,
  },
  title: {
    ...typography.headline.semiBold,
  },
  scrollView: {
    flex: 1,
  },
  scrollContent: {
    paddingHorizontal: spacing.md,
    paddingBottom: 40,
    gap: spacing.sm,
  },
  emptyState: {
    flex: 1,
    justifyContent: 'center',
    alignItems: 'center',
    paddingTop: 120,
    gap: spacing.md,
  },
  emptyText: {
    textAlign: 'center',
    lineHeight: 22,
  },
  jobCard: {
    borderRadius: radius.lg,
    padding: spacing.md,
    gap: spacing.sm,
  },
  jobHeader: {
    flexDirection: 'row',
    justifyContent: 'space-between',
    alignItems: 'center',
  },
  jobStatus: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: spacing.sm,
  },
  jobActions: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: spacing.md,
  },
  retryButton: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: spacing.xs,
  },
  statusText: {
    marginLeft: 26,
  },
  jobFooter: {
    flexDirection: 'row',
    justifyContent: 'space-between',
    marginLeft: 26,
  },
  progressContainer: {
    marginTop: spacing.xs,
    marginLeft: 26,
  },
  progressTrack: {
    height: 4,
    borderRadius: 2,
    overflow: 'hidden',
  },
  progressIndeterminate: {
    width: '30%',
    height: '100%',
    borderRadius: 2,
  },
});
