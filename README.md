Mattermost Community Continuous Profiling
====================================================

Mattermost community continuous profiling tool is a microservice designed to work as part of a k8s cronjob that runs cpu and memory profiles for Mattermost community (or any other Mattermost installation) and posts in a Mattermost channel.

## Environment Variables

The following environment variables need to be exported for the microservice to work:

- UPLOAD_API_URL:
This is the API url that should be used for the profiling attachment uploads. For example `http://community:8065/api/v4/files`. More information on Mattermost files API call can be found [here](https://api.mattermost.com/#tag/files).

- POST_API_URL:
This is the API url that should be used for the profiling attachment posts. For example `http://community:8065/api/v4/posts`. More information on Mattermost posts API call can be found [here](https://api.mattermost.com/#tag/posts).

- MATTERMOST_PROFILE_TARGETS:
The k8s service name of the target that the profile will run against to. For example `community`

- PROFILING_TIME:
How long the CPU profiling should run for. For example `30` for 30 seconds.

- CHANNEL_ID:
The ID of the channel that the microservice should post the profiles. It is best to keep this in a k8s secret.

- TOKEN:
The Token for the Mattermost Bot that will authenticate and upload/post the profile attachments. It is best to keep this in a k8s secret. More information on Mattermost bots can be found [here](https://docs.mattermost.com/developer/bot-accounts.html).
