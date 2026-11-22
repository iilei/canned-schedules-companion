# Using the JSON

Every valid schedule produces `schedule.json`: the canonical week, its transitions with cron expressions, and the rules
the schedule was checked against. The [canned-schedules docs](https://iilei.github.io/canned-schedules/) show how
to put it to work:

| Looking for                                 | Where                                                                                                          |
| ------------------------------------------- | -------------------------------------------------------------------------------------------------------------- |
| What each field means                       | [Output](https://iilei.github.io/canned-schedules/output.html)                                                 |
| The JSON Schema                             | [`schema/output.schema.json`](https://github.com/iilei/canned-schedules/blob/master/schema/output.schema.json) |
| Scheduling capacity on AWS, step by step    | [AWS Auto Scaling scheduled actions](https://iilei.github.io/canned-schedules/aws.html)                        |
| Working Terraform and AWS CLI files to copy | [`examples/aws`](https://github.com/iilei/canned-schedules/tree/master/examples/aws)                           |
