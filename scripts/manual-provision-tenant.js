/**
 * Manual Railway Service Provisioning for tenant-44-0dc9c25b
 * This creates MongoDB, Redis, and RoomService for your tenant
 */

const RAILWAY_TOKEN = process.env.RAILWAY_TOKEN || 'YOUR_TOKEN_HERE';
const PROJECT_ID = process.env.RAILWAY_PROJECT_ID || 'YOUR_PROJECT_ID_HERE';
const TENANT_ID = 'tenant-44-0dc9c25b';

async function graphqlQuery(query, variables = {}) {
  const response = await fetch('https://backboard.railway.app/graphql/v2', {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
      'Authorization': `Bearer ${RAILWAY_TOKEN}`
    },
    body: JSON.stringify({ query, variables })
  });

  const data = await response.json();
  if (data.errors) {
    throw new Error(data.errors[0].message);
  }
  return data.data;
}

function generatePassword() {
  const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789";
  return Array.from({length: 32}, () =>
    charset[Math.floor(Math.random() * charset.length)]
  ).join('');
}

async function createService(name, image) {
  const query = `
    mutation($projectId: String!, $name: String!, $image: String!) {
      serviceCreate(input: {
        projectId: $projectId
        name: $name
        source: { image: $image }
      }) {
        id
        name
      }
    }
  `;

  const result = await graphqlQuery(query, {
    projectId: PROJECT_ID,
    name: name,
    image: image
  });

  return result.serviceCreate;
}

async function setEnvironmentVariables(serviceId, variables) {
  const query = `
    mutation($input: VariableCollectionUpsertInput!) {
      variableCollectionUpsert(input: $input)
    }
  `;

  await graphqlQuery(query, {
    input: {
      projectId: PROJECT_ID,
      environmentId: process.env.RAILWAY_ENVIRONMENT_ID,
      serviceId: serviceId,
      variables: variables
    }
  });
}

async function main() {
  console.log('🔧 Manual Service Provisioning for tenant-44-0dc9c25b\n');

  if (RAILWAY_TOKEN === 'YOUR_TOKEN_HERE') {
    console.log('❌ Please set environment variables:');
    console.log('   RAILWAY_TOKEN=your_token');
    console.log('   RAILWAY_PROJECT_ID=your_project_id');
    console.log('   RAILWAY_ENVIRONMENT_ID=your_environment_id');
    return;
  }

  try {
    // 1. Create MongoDB
    console.log('1️⃣ Creating MongoDB service...');
    const mongoService = await createService(`${TENANT_ID}-mongo`, 'mongo:6');
    console.log(`✅ MongoDB created: ${mongoService.id}`);

    // Set MongoDB environment variables
    await setEnvironmentVariables(mongoService.id, {
      MONGO_INITDB_ROOT_USERNAME: 'admin',
      MONGO_INITDB_ROOT_PASSWORD: generatePassword()
    });
    console.log('✅ MongoDB configured');

    // 2. Create Redis
    console.log('\n2️⃣ Creating Redis service...');
    const redisService = await createService(`${TENANT_ID}-redis`, 'redis:7');
    console.log(`✅ Redis created: ${redisService.id}`);

    // Set Redis environment variables
    await setEnvironmentVariables(redisService.id, {
      REDIS_PASSWORD: generatePassword()
    });
    console.log('✅ Redis configured');

    // 3. Create RoomService
    console.log('\n3️⃣ Creating RoomService...');
    const roomService = await createService(TENANT_ID, 'chempik1234/roomservice:latest');
    console.log(`✅ RoomService created: ${roomService.id}`);

    // Set RoomService environment variables
    const mongoURL = `${mongoService.id}.railway.internal`;
    const redisURL = `${redisService.id}.railway.internal`;

    await setEnvironmentVariables(roomService.id, {
      // Service configuration
      'ROOM_SERVICE_GRPC_PORT': '50050',
      'ROOM_SERVICE_USE_AUTH': 'true',
      'ROOM_SERVICE_API_KEY': generatePassword(),
      'ROOM_SERVICE_LOG_LEVEL': 'info',

      // MongoDB configuration
      'ROOM_SERVICE_ROOMS_MONGODB_DATABASE': 'rooms_db',
      'ROOM_SERVICE_ROOMS_MONGODB_ROOMS_COLLECTION': 'rooms',
      'ROOM_SERVICE_MONGODB_HOSTS': mongoURL,
      'ROOM_SERVICE_MONGODB_USERNAME': 'admin',
      'ROOM_SERVICE_MONGODB_PASSWORD': generatePassword(),

      // Redis configuration
      'ROOM_SERVICE_REDIS_ADDR': redisURL,
      'ROOM_SERVICE_REDIS_PASSWORD': generatePassword(),
      'ROOM_SERVICE_REDIS_DB': '0',

      // Tenant identification
      'TENANT_ID': TENANT_ID
    });
    console.log('✅ RoomService configured');

    console.log('\n🎉 All services created successfully!');
    console.log('\n📋 Service IDs:');
    console.log(`   MongoDB: ${mongoService.id}`);
    console.log(`   Redis: ${redisService.id}`);
    console.log(`   RoomService: ${roomService.id}`);
    console.log('\n⏳ Next steps:');
    console.log('1. Wait 2-3 minutes for services to deploy');
    console.log('2. Check deployment status in Railway dashboard');
    console.log('3. Test the tic-tac-toe game');

  } catch (error) {
    console.error('💥 Error:', error.message);
    console.error('\n🔍 Troubleshooting:');
    console.error('- Check your RAILWAY_TOKEN has project access');
    console.error('- Verify PROJECT_ID and ENVIRONMENT_ID are correct');
    console.error('- Ensure Railway project has sufficient quota');
  }
}

main();
